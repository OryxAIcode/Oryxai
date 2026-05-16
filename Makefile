.PHONY: help build test lint clean

BINARY_DIR    := bin
GO            := go
GOFLAGS       := -trimpath
VERSION       := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT        := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG   := github.com/OryxAIcode/Oryxai/internal/version
LDFLAGS       := -s -w \
                 -X $(VERSION_PKG).Version=$(VERSION) \
                 -X $(VERSION_PKG).Commit=$(COMMIT) \
                 -X $(VERSION_PKG).BuildTime=$(BUILD_TIME)

.DEFAULT_GOAL := help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "}; /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the oryxai binary
	@mkdir -p $(BINARY_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/oryxai ./cmd/oryxai

test: ## Run tests
	$(GO) test -race -timeout 60s ./...

lint: ## Run go vet
	$(GO) vet ./...

clean: ## Remove build artifacts
	rm -rf $(BINARY_DIR)/
	$(GO) clean -cache -testcache
