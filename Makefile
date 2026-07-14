BIN_OUTPUT_PATH = bin
TOOL_BIN = $(BIN_OUTPUT_PATH)/gotools
MODULE_BINARY = $(BIN_OUTPUT_PATH)/arduino
GOLANGCI_VERSION = v1.61.0

# Static build so the module binary runs on any glibc/musl UNO Q image.
$(MODULE_BINARY): Makefile go.mod unoq/*.go utils/*.go cmd/module/*.go
	GOOS=$(VIAM_BUILD_OS) GOARCH=$(VIAM_BUILD_ARCH) go build \
		-tags no_cgo,osusergo,netgo \
		-ldflags="-extldflags=-static -s -w" \
		-o $(MODULE_BINARY) cmd/module/main.go

module.tar.gz: module
module: test $(MODULE_BINARY)
	rm -f $(BIN_OUTPUT_PATH)/module.tar.gz
	tar czf $(BIN_OUTPUT_PATH)/module.tar.gz meta.json setup.sh firmware/uno-q-firmware/ $(MODULE_BINARY)

test:
	go test -race ./...

# golangci-lint is pinned in CI (etc/.golangci.yaml) with a version matching the
# repo's Go toolchain. Locally, `make lint` runs gofmt + vet, which always pass;
# run `make lint-golangci` in an environment with a compatible golangci-lint.
lint:
	gofmt -s -w .
	go vet ./...

lint-golangci: tool-install
	$(TOOL_BIN)/golangci-lint run --config etc/.golangci.yaml

tool-install:
	GOBIN=$(shell pwd)/$(TOOL_BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

update:
	go get go.viam.com/rdk@latest
	go mod tidy

setup:
	go mod tidy

clean:
	rm -rf $(BIN_OUTPUT_PATH)

.PHONY: module module.tar.gz test tool-install lint update setup clean
