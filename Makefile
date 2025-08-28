.PHONY: lint
lint:
	go mod tidy
	go fmt ./...
	golangci-lint fmt --no-config --enable gofmt,goimports
	golangci-lint run --no-config --fix