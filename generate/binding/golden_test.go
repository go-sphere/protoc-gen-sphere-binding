package binding

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sphere/protoc-gen-sphere-binding/generate/internal/testutil"
)

var updateGolden = flag.Bool("update-golden", false, "update golden files")

type goldenCase struct {
	name       string
	pbFile     string // testdata/pb/<name>.pb
	protoName  string // proto path inside the descriptor set
	inputFile  string // testdata/gen/<name>.pb.go (raw protoc-gen-go output)
	wantChange bool   // whether the plugin rewrites the input
	goldenFile string // testdata/golden/<name>.pb.go (empty when wantChange is false)
	config     func() *Config
}

func goldenCases() []goldenCase {
	return []goldenCase{
		{
			name:       "basic",
			pbFile:     "testdata/pb/basic.pb",
			protoName:  "basic.proto",
			inputFile:  "testdata/gen/basic.pb.go",
			wantChange: true,
			goldenFile: "testdata/golden/basic.pb.go",
		},
		{
			// Same proto as basic, but json tags are kept and extra aliases are
			// registered, so query fields also get a form tag and uri fields a
			// path tag.
			name:       "basic_aliases",
			pbFile:     "testdata/pb/basic.pb",
			protoName:  "basic.proto",
			inputFile:  "testdata/gen/basic.pb.go",
			wantChange: true,
			goldenFile: "testdata/golden/basic_aliases.pb.go",
			config: func() *Config {
				return &Config{
					AutoRemoveJson: false,
					BindingAliases: map[string][]string{
						"query": {"form"},
						"uri":   {"path"},
					},
				}
			},
		},
		{
			name:       "tags",
			pbFile:     "testdata/pb/tags.pb",
			protoName:  "tags.proto",
			inputFile:  "testdata/gen/tags.pb.go",
			wantChange: true,
			goldenFile: "testdata/golden/tags.pb.go",
		},
		{
			name:       "oneof",
			pbFile:     "testdata/pb/oneof.pb",
			protoName:  "oneof.proto",
			inputFile:  "testdata/gen/oneof.pb.go",
			wantChange: true,
			goldenFile: "testdata/golden/oneof.pb.go",
		},
		{
			// No sphere.binding options, so the plugin must leave the file alone.
			name:       "no_binding",
			pbFile:     "testdata/pb/no_binding.pb",
			protoName:  "no_binding.proto",
			inputFile:  "testdata/gen/no_binding.pb.go",
			wantChange: false,
		},
	}
}

// generate runs the full extract -> retag pipeline for a single case against the
// committed protoc-gen-go input and returns the rewritten source plus whether
// anything changed.
func (tt goldenCase) generate(t *testing.T) ([]byte, bool) {
	t.Helper()

	set := testutil.LoadDescriptorSet(t, tt.pbFile)
	plugin := testutil.MustCreatePlugin(t, set, tt.protoName)
	file := testutil.FileToGenerate(t, plugin)

	cfg := DefaultConfig()
	if tt.config != nil {
		cfg = tt.config()
	}

	tags, err := extractFile(file, cfg)
	if err != nil {
		t.Fatalf("extractFile(%s) failed: %v", tt.name, err)
	}

	src, err := os.ReadFile(tt.inputFile)
	if err != nil {
		t.Fatalf("failed to read input fixture %q (run `make testdata`): %v", tt.inputFile, err)
	}

	content, changed, err := RetagSource(tt.inputFile, src, tags)
	if err != nil {
		t.Fatalf("RetagSource(%s) failed: %v", tt.name, err)
	}
	return content, changed
}

func TestGolden(t *testing.T) {
	for _, tt := range goldenCases() {
		t.Run(tt.name, func(t *testing.T) {
			content, changed := tt.generate(t)

			if !tt.wantChange {
				if changed {
					t.Fatalf("expected no rewrite, but the plugin changed the file")
				}
				return
			}
			if !changed {
				t.Fatal("expected the plugin to rewrite the file, but nothing changed")
			}

			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(tt.goldenFile), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(tt.goldenFile, content, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated golden file: %s", tt.goldenFile)
				return
			}

			expected, err := os.ReadFile(tt.goldenFile)
			if err != nil {
				t.Fatalf("failed to read golden file (run `make update-golden` to create): %v", err)
			}
			if diff := firstDiff(string(expected), string(content)); diff != "" {
				t.Errorf("generated content mismatch for %s (run `make update-golden` to refresh):\n%s", tt.name, diff)
			}
		})
	}
}

// TestGoldenDeterministic guards against non-deterministic output (e.g. map
// iteration order leaking into tag ordering) by retagging twice and comparing
// bytes.
func TestGoldenDeterministic(t *testing.T) {
	for _, tt := range goldenCases() {
		if !tt.wantChange {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			first, _ := tt.generate(t)
			second, _ := tt.generate(t)
			if string(first) != string(second) {
				t.Errorf("non-deterministic output for %s:\n%s", tt.name, firstDiff(string(first), string(second)))
			}
		})
	}
}

// firstDiff returns a human-readable description of the first differing line
// between want and got, or "" when they are equal. It avoids pulling in
// github.com/google/go-cmp as a module dependency.
func firstDiff(want, got string) string {
	if want == got {
		return ""
	}
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	for i := 0; i < len(wl) && i < len(gl); i++ {
		if wl[i] != gl[i] {
			return fmt.Sprintf("first difference at line %d:\n  want: %q\n  got:  %q", i+1, wl[i], gl[i])
		}
	}
	return fmt.Sprintf("line count differs: want %d lines, got %d lines", len(wl), len(gl))
}
