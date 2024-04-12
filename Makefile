GOROOT := $(shell go env GOROOT)
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
LDFLAGS := -ldflags "-s -w"
INSTALL_DIR := /usr/local/bin

.PHONY: help fmt

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9\/_\.-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

spt: ## Build the binary
	for CMD in `ls cmd`; do \
		GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) cmd/$$CMD/main.go; \
	done

spt.wasm: ## Build the wasm file
	GOOS=js GOARCH=wasm make spt
	cp $(GOROOT)/misc/wasm/wasm_exec.js .

install: spt ## Install the binary
	cp main $(INSTALL_DIR)/spt

clean: ## Clean the build
	rm -f main spt.wasm wasm_exec.js

fmt: ## Format the code
	go fmt
