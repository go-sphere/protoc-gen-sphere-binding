GO ?= go
GOLANGCI_LINT ?= golangci-lint
NILAWAY ?= nilaway

DIRECT_DEPS_TEMPLATE := {{if and (not .Main) (not .Indirect) (not .Replace)}}{{.Path}}{{end}}

# Resolve go-sphere modules straight from GitHub, bypassing the module proxy
# and its cached "@latest", which lags behind freshly pushed tags.
DIRECT_ORIGIN := GOPRIVATE=github.com/go-sphere/*

.DEFAULT_GOAL := check

TESTDATA := generate/binding/testdata
TOOLS_BIN := $(CURDIR)/.tools

# Pin the fixture generator to the same protobuf module version as go.mod / CI
# (and the committed golden headers). A newer protoc-gen-go on PATH would only
# change the version comment, but that still fails byte-for-byte golden tests.
PROTOC_GEN_GO_VERSION := $(shell $(GO) list -m -f '{{.Version}}' google.golang.org/protobuf)

.PHONY: deps-update tidy tidy-check fmt

deps-update:
	@GOWORK=off $(DIRECT_ORIGIN) $(GO) mod tidy; \
	deps="$$(GOWORK=off $(DIRECT_ORIGIN) $(GO) list -m -f '$(DIRECT_DEPS_TEMPLATE)' all)"; \
	if [ -n "$$deps" ]; then GOWORK=off $(DIRECT_ORIGIN) $(GO) get -u $$deps; fi
	GOWORK=off $(DIRECT_ORIGIN) $(GO) mod tidy

tidy:
	GOWORK=off $(GO) mod tidy

# Non-mutating counterpart of tidy, for CI: fails if go.mod/go.sum are not
# what a consumer would resolve.
tidy-check:
	GOWORK=off $(GO) mod tidy -diff

fmt:
	$(GO) fmt ./...
	$(GOLANGCI_LINT) fmt --no-config --enable gofmt --enable goimports

# pb/ and gen/ are gitignored and rebuilt here. buf generate must use the pinned
# protoc-gen-go, not whatever happens to be first on PATH.
.PHONY: testdata
testdata:
	@mkdir -p $(TESTDATA)/pb $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	@for p in $(TESTDATA)/proto/*.proto; do \
		name=$$(basename $$p .proto); \
		echo "building $$p -> $(TESTDATA)/pb/$$name.pb"; \
		buf build $(TESTDATA) --path $$p --as-file-descriptor-set \
			-o $(TESTDATA)/pb/$$name.pb || exit 1; \
	done
	PATH="$(TOOLS_BIN):$$PATH" buf generate $(TESTDATA) --template $(TESTDATA)/buf.gen.yaml -o $(TESTDATA)

.PHONY: update-golden
# Scoped to the binding package: it is the only one that defines -update-golden,
# so passing the flag to ./generate/... would fail the testutil test binary.
update-golden: testdata
	$(GO) test ./generate/binding/ -run TestGolden -update-golden

.PHONY: build test
build:
	$(GO) build ./...

test: testdata
	$(GO) test ./...

.PHONY: lint check install
lint:
	$(GOLANGCI_LINT) fmt --no-config --enable gofmt --enable goimports --diff
	$(GO) vet ./...
	$(GOLANGCI_LINT) run --no-config
	$(NILAWAY) -include-pkgs="$$($(GO) list -m)" ./...

check: tidy-check
	$(MAKE) lint
	$(MAKE) test

install:
	$(GO) install .
