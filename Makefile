MODULE := $(shell go list -m)

TESTDATA := generate/binding/testdata
TOOLS_BIN := $(CURDIR)/.tools
# Pin the fixture generator to the same protobuf module version as go.mod / CI
# (and the committed golden headers). A newer protoc-gen-go on PATH would only
# change the version comment, but that still fails byte-for-byte golden tests.
PROTOC_GEN_GO_VERSION := $(shell go list -m -f '{{.Version}}' google.golang.org/protobuf)

# pb/ and gen/ are gitignored and rebuilt here. buf generate must use the pinned
# protoc-gen-go, not whatever happens to be first on PATH.
.PHONY: testdata
testdata:
	@mkdir -p $(TESTDATA)/pb $(TOOLS_BIN)
	GOBIN=$(TOOLS_BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
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
	go test ./generate/binding/ -run TestGolden -update-golden

.PHONY: test
test: testdata
	go test ./...

.PHONY: lint
lint:
	go fix ./...
	go fmt ./...
	go vet ./...
	go get ./...
	go test ./...
	go mod tidy
	golangci-lint fmt --no-config --enable gofmt,goimports
	golangci-lint run --no-config --fix
	nilaway -include-pkgs="$(MODULE)" ./...

.PHONY: install
install:
	go install .
