MODULE := $(shell go list -m)

TESTDATA := generate/binding/testdata

# Compile the test fixtures into committed artifacts. Only this target needs buf
# and protoc-gen-go; `go test` runs against the committed pb/ and gen/ files.
#
# Two outputs are produced per fixture:
#   pb/<name>.pb     - FileDescriptorSet (bundles deps + source info) that the
#                      plugin reads to extract binding options.
#   gen/<name>.pb.go - raw protoc-gen-go output, the *input* the plugin rewrites.
# The retagged result is the golden file under golden/, refreshed separately by
# the update-golden target.
.PHONY: testdata
testdata:
	@mkdir -p $(TESTDATA)/pb
	@for p in $(TESTDATA)/proto/*.proto; do \
		name=$$(basename $$p .proto); \
		echo "building $$p -> $(TESTDATA)/pb/$$name.pb"; \
		buf build $(TESTDATA) --path $$p --as-file-descriptor-set \
			-o $(TESTDATA)/pb/$$name.pb || exit 1; \
	done
	buf generate $(TESTDATA) --template $(TESTDATA)/buf.gen.yaml -o $(TESTDATA)

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
