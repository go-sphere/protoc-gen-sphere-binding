# Hybrid Testing Plan for Protoc Plugins

## Overview

For multiple `protoc-gen-*` plugin projects, use a hybrid testing strategy that combines **precompiled Proto + hand-written descriptors + golden files**. This balances test coverage, maintenance cost, and dependency simplicity.

## Layered Test Architecture

```
+-----------------------------------------+
| Layer 3: Golden file regression tests    |
| - Prevent unexpected generated changes   |
| - Compare complete output                |
| - Update with `go test -update-golden`   |
+-----------------------------------------+
| Layer 2: Functional integration tests    |
| - Precompile .proto files to .pb files   |
| - Test full proto syntax and extensions  |
| - Verify generated code structure        |
+-----------------------------------------+
| Layer 1: Unit tests                      |
| - Use hand-written descriptors           |
| - Quickly verify edge cases              |
| - No external file dependencies          |
| - Test errors, empty values, skip logic  |
+-----------------------------------------+
```

## Directory Structure

Each plugin project should use a consistent test directory layout:

```
protoc-gen-*/
|-- generate/
|   `-- <plugin-name>/
|       |-- <plugin-name>.go          # Main generation logic
|       |-- <plugin-name>_test.go     # Unit tests (Layer 1)
|       |-- golden_test.go            # Golden file tests (Layer 3)
|       `-- testdata/
|           |-- buf.yaml              # buf module + deps for the fixtures
|           |-- buf.lock              # pinned dep versions (buf dep update)
|           |-- proto/                # Test .proto source files
|           |   |-- basic.proto
|           |   |-- complex.proto
|           |   `-- edge_cases.proto
|           |-- pb/                   # Descriptor sets precompiled by buf
|           |   |-- basic.pb
|           |   |-- complex.pb
|           |   `-- edge_cases.pb
|           `-- golden/               # Expected output (Layer 3)
|               |-- basic.sphere.*.pb.go
|               |-- complex.sphere.*.pb.go
|               `-- empty.sphere.*.pb.go
|-- Makefile                          # Shared testdata compilation rules
`-- go.mod
```

## Three Testing Layers

### Layer 1: Unit Tests With Hand-Written FileDescriptorProto

**Use cases**:
- Quickly verify skip logic for empty enums or messages.
- Test default behavior when no extension options are present.
- Verify edge cases such as streaming methods and empty service names.

**Characteristics**:
- No external file dependencies.
- Fast execution.
- Cannot test custom protobuf extension options.

**Example code**:
```go
// generate/http/http_test.go
package http

import (
    "testing"

    "google.golang.org/protobuf/compiler/protogen"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/descriptorpb"
    "google.golang.org/protobuf/types/pluginpb"
)

// TestGenerateFile_EmptyService verifies that an empty service returns nil.
func TestGenerateFile_EmptyService(t *testing.T) {
    fd := &descriptorpb.FileDescriptorProto{
        Name:    proto.String("empty.proto"),
        Package: proto.String("api.v1"),
        Options: &descriptorpb.FileOptions{
            GoPackage: proto.String("github.com/example/api"),
        },
    }

    req := &pluginpb.CodeGeneratorRequest{
        FileToGenerate: []string{"empty.proto"},
        ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
    }

    plugin, err := protogen.Options{}.New(req)
    if err != nil {
        t.Fatalf("failed to create plugin: %v", err)
    }

    // Safe here: one hand-written file with no imports, so it is plugin.Files[0]
    // and Generate is true. With precompiled .pb files (buf bundles imports) use
    // testutil.FileToGenerate instead — see the pitfall note in Layer 2.
    file := plugin.Files[0]
    genFile, err := GenerateFile(plugin, file, &Config{Omitempty: true})
    if err != nil {
        t.Fatalf("GenerateFile failed: %v", err)
    }

    if genFile != nil {
        t.Error("expected nil for empty service, got non-nil")
    }
}

// TestGenerateFile_StreamingOnly verifies that streaming-only methods are ignored.
func TestGenerateFile_StreamingOnly(t *testing.T) {
    fd := &descriptorpb.FileDescriptorProto{
        Name:    proto.String("streaming.proto"),
        Package: proto.String("api.v1"),
        Options: &descriptorpb.FileOptions{
            GoPackage: proto.String("github.com/example/api"),
        },
        MessageType: []*descriptorpb.DescriptorProto{
            {
                Name: proto.String("StreamRequest"),
                Field: []*descriptorpb.FieldDescriptorProto{
                    {
                        Name:   proto.String("data"),
                        Number: proto.Int32(1),
                        Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
                        Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
                    },
                },
            },
            {
                Name: proto.String("StreamResponse"),
                Field: []*descriptorpb.FieldDescriptorProto{
                    {
                        Name:   proto.String("result"),
                        Number: proto.Int32(1),
                        Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
                        Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
                    },
                },
            },
        },
        Service: []*descriptorpb.ServiceDescriptorProto{
            {
                Name: proto.String("StreamService"),
                Method: []*descriptorpb.MethodDescriptorProto{
                    {
                        Name:            proto.String("StreamData"),
                        InputType:       proto.String(".api.v1.StreamRequest"),
                        OutputType:      proto.String(".api.v1.StreamResponse"),
                        ClientStreaming: proto.Bool(true),
                    },
                },
            },
        },
    }

    req := &pluginpb.CodeGeneratorRequest{
        FileToGenerate: []string{"streaming.proto"},
        ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
    }

    plugin, err := protogen.Options{}.New(req)
    if err != nil {
        t.Fatalf("failed to create plugin: %v", err)
    }

    // Safe here: single hand-written file, no imports (see note above).
    file := plugin.Files[0]
    genFile, err := GenerateFile(plugin, file, &Config{Omitempty: false})
    if err != nil {
        t.Fatalf("GenerateFile failed: %v", err)
    }

    if genFile != nil {
        t.Error("expected nil for streaming-only service, got non-nil")
    }
}
```

### Layer 2: Functional Integration Tests With Precompiled .pb Files

**Use cases**:
- Test complete proto files containing custom protobuf extensions.
- Verify extension options such as `google.api.http` and `errors.default_status`.
- Test complex nested messages, multiple services, and multiple methods.

**Workflow**:
1. Write test `.proto` files containing extension options.
2. Run `make testdata` to compile them to `.pb` `FileDescriptorSet` files with `buf build`.
3. Load the `.pb` files in tests and construct `CodeGeneratorRequest` values.
4. Verify that generated code contains the expected key structures.

**Shared Makefile rules**:
```makefile
# Root Makefile
TESTDATA := generate/<plugin>/testdata

# Compile each fixture into a self-contained FileDescriptorSet under pb/.
.PHONY: testdata
testdata:
	@mkdir -p $(TESTDATA)/pb
	@for proto in $(TESTDATA)/proto/*.proto; do \
		name=$$(basename $$proto .proto); \
		echo "Building $$proto -> $(TESTDATA)/pb/$$name.pb"; \
		buf build $(TESTDATA) --path $$proto --as-file-descriptor-set \
			-o $(TESTDATA)/pb/$$name.pb || exit 1; \
	done

.PHONY: test
test: testdata
	go test ./...
```

> **Why `buf` instead of `protoc`.** Imports such as `google/api/*.proto` and
> `buf/validate/validate.proto` are **not** in any Go module, so a pure-`protoc`
> setup has to vendor them by hand. `buf build` avoids that: it resolves every
> dependency from `buf.lock` (no vendored protos), bundles the imports into the
> output, and keeps source info on by default (so comments survive — no
> `--include_source_info` flag needed). Set it up once:
>
> 1. Add a `buf.yaml` next to the fixtures declaring the module and deps:
>    ```yaml
>    version: v2
>    modules:
>      - path: proto
>    deps:
>      - buf.build/googleapis/googleapis
>      - buf.build/go-sphere/binding
>      - buf.build/bufbuild/protovalidate
>    ```
> 2. Run `buf dep update` (in the `testdata/` dir) to pin `buf.lock`; commit it.
> 3. Build each fixture (see the Makefile rule above). `--as-file-descriptor-set`
>    strips buf-only metadata so the output unmarshals straight into
>    `descriptorpb.FileDescriptorSet`.
>
> Only `make testdata` needs `buf`; `go test` runs against the committed `*.pb`
> files. (Pure-`protoc` fallback: vendor the missing `.proto` files into a
> committed `deps/` tree and pass `--include_imports --include_source_info`.)

**Test loading helpers** reused by each project:

> **Important:** A `buf build --as-file-descriptor-set` output contains **every
> dependency** (e.g. `google/protobuf/descriptor.proto` and the extension
> `.proto`), with dependencies ordered **before** the target file. Therefore:
> - Pass the **entire** `set.File` slice to `ProtoFile` — passing only one
>   descriptor makes `protogen.Options{}.New` fail to resolve imports.
> - **Never** use `plugin.Files[0]` to pick the target file; it is usually a
>   dependency. Select the file whose `Generate` flag is `true`.

```go
// generate/internal/testutil/testutil.go
package testutil

import (
    "os"
    "testing"

    "google.golang.org/protobuf/compiler/protogen"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/descriptorpb"
    "google.golang.org/protobuf/types/pluginpb"
)

// LoadDescriptorSet reads and unmarshals a FileDescriptorSet produced by
// `buf build --as-file-descriptor-set`. The whole set (including dependencies)
// must be kept so imports can be resolved later.
func LoadDescriptorSet(t *testing.T, path string) *descriptorpb.FileDescriptorSet {
    t.Helper()

    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("failed to read descriptor set %q: %v", path, err)
    }

    var set descriptorpb.FileDescriptorSet
    if err := proto.Unmarshal(data, &set); err != nil {
        t.Fatalf("failed to unmarshal descriptor set %q: %v", path, err)
    }
    if len(set.File) == 0 {
        t.Fatalf("descriptor set %q contains no files", path)
    }
    return &set
}

// MustCreatePlugin builds a real *protogen.Plugin from a descriptor set. The set
// must include every dependency; fileToGenerate is the proto path (relative to
// the buf module root) that should be generated.
//
// CompilerVersion is pinned so generated headers stay deterministic and do not
// depend on the host toolchain version (an unset version renders as "(unknown)").
func MustCreatePlugin(t *testing.T, set *descriptorpb.FileDescriptorSet, fileToGenerate string) *protogen.Plugin {
    t.Helper()

    req := &pluginpb.CodeGeneratorRequest{
        FileToGenerate: []string{fileToGenerate},
        ProtoFile:      set.File, // all files, dependencies first
        CompilerVersion: &pluginpb.Version{
            Major: proto.Int32(5),
            Minor: proto.Int32(29),
            Patch: proto.Int32(0),
        },
    }

    plugin, err := protogen.Options{}.New(req)
    if err != nil {
        t.Fatalf("failed to create plugin for %q: %v", fileToGenerate, err)
    }
    return plugin
}

// FileToGenerate returns the single file marked for generation. Always use this
// instead of plugin.Files[0]: with bundled imports, plugin.Files[0] is a
// dependency (e.g. descriptor.proto), not the target.
func FileToGenerate(t *testing.T, plugin *protogen.Plugin) *protogen.File {
    t.Helper()
    for _, f := range plugin.Files {
        if f.Generate {
            return f
        }
    }
    t.Fatal("no file marked for generation")
    return nil
}
```

**Layer 2 test example**:
```go
// generate/errors/errors_test.go
package errors

import (
    "strings"
    "testing"

    "github.com/go-sphere/protoc-gen-sphere-errors/generate/internal/testutil"
    "google.golang.org/protobuf/compiler/protogen"
)

func TestGenerateFile_WithErrorEnums(t *testing.T) {
    set := testutil.LoadDescriptorSet(t, "testdata/pb/basic_errors.pb")
    plugin := testutil.MustCreatePlugin(t, set, "basic_errors.proto")

    file := testutil.FileToGenerate(t, plugin)
    genFile, err := GenerateFile(plugin, file, &Config{
        NewErrorsFunc: protogen.GoIdent{
            GoName:       "NewError",
            GoImportPath: "github.com/go-sphere/httpx",
        },
    })
    if err != nil {
        t.Fatalf("GenerateFile failed: %v", err)
    }
    if genFile == nil {
        t.Fatal("expected generated file, got nil")
    }

    // GeneratedFile.Content() runs gofmt and resolves imports; it returns
    // ([]byte, error), so the error must be checked.
    raw, err := genFile.Content()
    if err != nil {
        t.Fatalf("failed to format generated content: %v", err)
    }
    content := string(raw)

    // Verify key generated methods.
    expectedMethods := []string{
        "func (e UserError) Error() string",
        "func (e UserError) GetCode() int32",
        "func (e UserError) GetStatus() int32",
        "func (e UserError) GetMessage() string",
        "func (e UserError) Join(errs ...error) error",
        "func (e UserError) JoinWithMessage(msg string, errs ...error) error",
    }

    for _, method := range expectedMethods {
        if !strings.Contains(content, method) {
            t.Errorf("generated content missing method: %q", method)
        }
    }
}

func TestGenerateFile_SkipsNormalEnums(t *testing.T) {
    set := testutil.LoadDescriptorSet(t, "testdata/pb/mixed_enums.pb")
    plugin := testutil.MustCreatePlugin(t, set, "mixed_enums.proto")

    file := testutil.FileToGenerate(t, plugin)
    genFile, err := GenerateFile(plugin, file, &Config{
        NewErrorsFunc: protogen.GoIdent{
            GoName:       "NewError",
            GoImportPath: "github.com/go-sphere/httpx",
        },
    })
    if err != nil {
        t.Fatalf("GenerateFile failed: %v", err)
    }
    if genFile == nil {
        t.Fatal("expected generated file for mixed enums, got nil")
    }

    raw, err := genFile.Content()
    if err != nil {
        t.Fatalf("failed to format generated content: %v", err)
    }
    content := string(raw)

    // Include the error enum only, not the normal enum.
    if !strings.Contains(content, "AuthError") {
        t.Error("expected AuthError in generated content")
    }
    if strings.Contains(content, "Color") {
        t.Error("unexpected normal enum Color in generated content")
    }
}
```

### Layer 3: Golden File Regression Tests

**Use cases**:
- Compare complete output and prevent unexpected changes.
- Ensure generated results stay the same during refactors.
- Quickly detect behavior changes during version upgrades.

**Implementation**:
```go
// generate/http/golden_test.go
package http

import (
    "flag"
    "os"
    "path/filepath"
    "testing"

    "github.com/google/go-cmp/cmp"
    "github.com/go-sphere/protoc-gen-sphere-http/generate/internal/testutil"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

func TestGoldenFiles(t *testing.T) {
    tests := []struct {
        name        string
        pbFile      string        // testdata/pb/xxx.pb
        protoName   string        // Original proto file name
        wantFile    bool          // Whether a generated file is expected
        goldenFile  string        // testdata/golden/xxx.sphere.http.pb.go
    }{
        {
            name:       "basic_service",
            pbFile:     "testdata/pb/basic.pb",
            protoName:  "basic.proto",
            wantFile:   true,
            goldenFile: "testdata/golden/basic.sphere.http.pb.go",
        },
        {
            name:       "complex_bindings",
            pbFile:     "testdata/pb/complex.pb",
            protoName:  "complex.proto",
            wantFile:   true,
            goldenFile: "testdata/golden/complex.sphere.http.pb.go",
        },
        {
            name:       "no_http_annotations",
            pbFile:     "testdata/pb/no_http.pb",
            protoName:  "no_http.proto",
            wantFile:   false,
            goldenFile: "",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            set := testutil.LoadDescriptorSet(t, tt.pbFile)
            plugin := testutil.MustCreatePlugin(t, set, tt.protoName)

            file := testutil.FileToGenerate(t, plugin)
            genFile, err := GenerateFile(plugin, file, &Config{Omitempty: true})
            if err != nil {
                t.Fatalf("GenerateFile failed: %v", err)
            }

            if !tt.wantFile {
                if genFile != nil {
                    t.Errorf("expected no generated file, got one")
                }
                return
            }

            if genFile == nil {
                t.Fatal("expected generated file, got nil")
            }

            content, err := genFile.Content()
            if err != nil {
                t.Fatalf("failed to format generated content: %v", err)
            }

            // Update the golden file.
            if *updateGolden {
                if err := os.MkdirAll(filepath.Dir(tt.goldenFile), 0755); err != nil {
                    t.Fatal(err)
                }
                if err := os.WriteFile(tt.goldenFile, content, 0644); err != nil {
                    t.Fatal(err)
                }
                t.Logf("updated golden file: %s", tt.goldenFile)
                return
            }

            // Read and compare against the expected output.
            expected, err := os.ReadFile(tt.goldenFile)
            if err != nil {
                t.Fatalf("failed to read golden file (run with -update-golden to create): %v", err)
            }

            if diff := cmp.Diff(string(expected), string(content)); diff != "" {
                t.Errorf("generated content mismatch (-want +got):\n%s", diff)
            }
        })
    }
}
```

> `github.com/google/go-cmp` only produces a nicer diff; it is optional. To keep
> the module dependency-free, replace `cmp.Diff` with a plain
> `string(expected) != string(content)` comparison and report the first differing
> line yourself.

## Common Pitfalls

These bite almost every protoc-plugin test suite. Each one is already handled by
the helpers above.

1. **`plugin.Files[0]` is usually a dependency.** `protogen.Plugin.Files`
   contains an entry for *every* file in the request (including
   `google/protobuf/descriptor.proto` and any imported extension `.proto`),
   ordered dependencies-first. Only files listed in `FileToGenerate` have
   `Generate == true`. Always select the target via the `Generate` flag, never by
   index.

2. **The descriptor set must bundle imports, and you must pass the whole set.**
   Custom options (e.g. `default_status`) live in an imported `.proto`. `buf build`
   bundles every dependency by default; feed the **entire** `set.File` slice to
   `ProtoFile`. Passing a single descriptor makes `protogen.Options{}.New` fail to
   resolve imports.

3. **`GeneratedFile.Content()` returns `([]byte, error)`.** It runs gofmt and
   synthesizes the `import` block, so it can fail on malformed output. Always
   check the error — `string(genFile.Content())` does not compile.

4. **Extension references use the proto package, not the Go path.** With
   `package sphere.errors;` the options are `(sphere.errors.default_status)`. The
   `import` path is relative to the buf module root (e.g. `sphere/errors/errors.proto`).

5. **Pin `CompilerVersion` for stable golden headers.** Generated headers often
   embed the protoc version. Set a fixed `pluginpb.Version` in the request so
   golden files do not churn with the host toolchain (an unset version renders as
   `(unknown)`).

6. **Commit the fixtures.** `.pb` descriptor sets, golden files, and `buf.lock`
   must be checked in so `go test ./...` runs without `buf` installed (e.g. in
   CI). Only `make testdata` / `make update-golden` need `buf`. Verify they are
   not excluded by `.gitignore`.

7. **Keep source info so comments survive.** If the plugin emits anything derived
   from proto comments — Go doc comments, Swagger `@Description`, etc. — the
   descriptor set must include `source_code_info`. `buf build` keeps it by default
   (it is dropped only with `--exclude-source-info`). If you fall back to `protoc`,
   you must add `--include_source_info` explicitly, or the golden output silently
   loses every comment.

8. **Build a fully-populated `Config`, not a bare struct literal.** For
   protoc-gen-sphere the generated output references the router/context/handler
   and response types from `Config`'s `protogen.GoIdent` fields. A bare
   `&http.Config{Omitempty: true}` leaves those empty and produces
   non-representative golden files. Use `http.DefaultConfig()` (the same defaults
   main.go uses) and override only what a case needs:

   ```go
   cfg := http.DefaultConfig()
   cfg.Omitempty = false // e.g. exercise the auto-POST default route
   genFile, err := http.GenerateFile(plugin, file, cfg)
   ```

## Cross-Project Reuse Strategy

### 1. Shared Test Utility Library

Create a `protoc-gen-sphere-testutil` module or an internal shared package:

```
go-sphere/
|-- protoc-gen-sphere-testutil/      # Optional: standalone module
|   |-- go.mod
|   `-- testutil.go
|-- protoc-gen-sphere-errors/
|   `-- generate/
|       `-- internal/
|           `-- testutil/            # Or embedded in each project
|               `-- testutil.go      # Symlinked to shared code
|-- protoc-gen-sphere-http/
|   `-- generate/
|       `-- internal/
|           `-- testutil/
|               `-- testutil.go
`-- ...
```

### 2. Makefile Template

Each project's Makefile should include the shared `testdata` rules:

```makefile
# Project root Makefile template

MODULE := $(shell go list -m)
TESTDATA := generate/<plugin>/testdata

# Compile test fixtures with buf (resolves deps from $(TESTDATA)/buf.lock).
.PHONY: testdata
testdata:
	@mkdir -p $(TESTDATA)/pb
	@for proto in $(TESTDATA)/proto/*.proto; do \
		name=$$(basename $$proto .proto); \
		echo "Building $$proto -> $(TESTDATA)/pb/$$name.pb"; \
		buf build $(TESTDATA) --path $$proto --as-file-descriptor-set \
			-o $(TESTDATA)/pb/$$name.pb || exit 1; \
	done

.PHONY: test
test: testdata
	go test ./...

# Scope -update-golden to the package that defines the flag; passing it to
# ./... fails the test binaries that do not declare it.
.PHONY: update-golden
update-golden: testdata
	go test ./generate/<plugin>/ -run TestGolden -update-golden

.PHONY: lint
lint:
	go fix ./...
	go fmt ./...
	go vet ./...
	go test ./...
	go mod tidy
	golangci-lint fmt --no-config --enable gofmt,goimports
	golangci-lint run --no-config --fix
	nilaway -include-pkgs="$(MODULE)" ./...
```

### 3. Test Data Naming Conventions

| File name pattern | Purpose | Layer |
|-----------|------|-------|
| `basic_*.proto` | Basic functionality tests | 2, 3 |
| `complex_*.proto` | Complex scenarios, such as multiple services, multiple methods, and nesting | 2, 3 |
| `empty_*.proto` | Empty service or empty enum edge cases | 1, 2 |
| `invalid_*.proto` | Error handling tests | 1 |
| `all_types_*.proto` | Full type coverage tests | 2 |

## Implementation Checklist

For each new plugin project:

- [ ] Create `generate/<plugin>/testdata/proto/`.
- [ ] Add `generate/<plugin>/testdata/buf.yaml` listing the deps; run `buf dep update`.
- [ ] Write basic `.proto` test files containing extension options.
- [ ] Add `testdata` compilation rules (buf) to the Makefile.
- [ ] Create or reuse an `internal/testutil` package.
- [ ] Write Layer 1 unit tests with hand-written descriptors.
- [ ] Write Layer 2 functional tests that load `.pb` files.
- [ ] Write Layer 3 golden file tests.
- [ ] Run `make testdata` to generate initial `.pb` files.
- [ ] Run `make update-golden` to generate initial golden files.
- [ ] Commit all test data (including `buf.lock`) to version control.

## Example: Test Proto for the errors Plugin

> **Extension references must use the proto package name**, not the Go import
> path. `errors.proto` declares `package sphere.errors`, so the options are
> `(sphere.errors.default_status)` / `(sphere.errors.options)`. The `import`
> path resolves through the buf module: the `sphere/errors` dep is declared in
> `buf.yaml` / `buf.lock`, so the file resolves to `sphere/errors/errors.proto`.

```protobuf
// testdata/proto/basic_errors.proto
syntax = "proto3";

package api.v1;

import "sphere/errors/errors.proto";

option go_package = "github.com/example/api/v1";

// Basic error enum.
enum UserError {
    option (sphere.errors.default_status) = 400;

    USER_ERROR_UNSPECIFIED = 0;
    USER_ERROR_INVALID_ID = 1 [(sphere.errors.options) = {
        status: 400,
        reason: "invalid user id",
        message: "invalid user ID format"
    }];
    USER_ERROR_NOT_FOUND = 2 [(sphere.errors.options) = {
        status: 404,
        reason: "user not found",
        message: "user does not exist"
    }];
    USER_ERROR_PERMISSION_DENIED = 3 [(sphere.errors.options) = {
        status: 403,
        reason: "permission denied"
        // The message is empty, so the default should be used.
    }];
}

// Normal enum. It should not generate error code.
enum Status {
    STATUS_UNSPECIFIED = 0;
    STATUS_ACTIVE = 1;
    STATUS_INACTIVE = 2;
}

// Another error enum.
enum OrderError {
    option (sphere.errors.default_status) = 500;

    ORDER_ERROR_UNSPECIFIED = 0;
    ORDER_ERROR_OUT_OF_STOCK = 1 [(sphere.errors.options) = {
        status: 400,
        reason: "out of stock",
        message: "product is out of stock"
    }];
}
```

---

**Summary**: This hybrid plan uses three complementary testing layers to provide:
- **Fast feedback**: Layer 1 unit tests run in milliseconds.
- **Complete coverage**: Layer 2 tests real proto extension options.
- **Regression protection**: Layer 3 golden files prevent unexpected changes.
- **Low maintenance cost**: A consistent directory structure, helper functions, and Makefile rules can be reused across projects.
