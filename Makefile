# Determine root directory
ROOT_DIR=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

# Gather all .go files for use in dependencies below
GO_FILES=$(shell find $(ROOT_DIR) -name '*.go')

# Extract Go module name from go.mod
GOMODULE=$(shell grep ^module $(ROOT_DIR)/go.mod | awk '{ print $$2 }')

# The binary name matches the last path element of the module
BINARY=$(shell basename $(GOMODULE))

# Set version strings based on git tag and current ref
VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null)
COMMIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_LDFLAGS=-ldflags "-s -w \
	-X '$(GOMODULE)/internal/version.Version=$(VERSION)' \
	-X '$(GOMODULE)/internal/version.Commit=$(COMMIT_HASH)' \
	-X '$(GOMODULE)/internal/version.BuildDate=$(BUILD_DATE)'"

.PHONY: all build install clean mod-tidy fmt vet lint test generate help

# Default target
all: build ## Build the binary (default)

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build our program binary
# Depends on GO_FILES to determine when a rebuild is needed
$(BINARY): mod-tidy $(GO_FILES)
	CGO_ENABLED=0 go build \
		$(GO_LDFLAGS) \
		-o $(BINARY)$(if $(filter windows,$(GOOS)),.exe,) \
		.

build: $(BINARY) ## Build the dingoctl binary

install: build ## Build, then install the binary via 'go install'
	CGO_ENABLED=0 go install $(GO_LDFLAGS) .

clean: ## Remove compiled binaries
	rm -f $(BINARY) $(BINARY).exe
	rm -rf dist/

mod-tidy: ## Run go mod tidy
	# Needed to fetch new dependencies and add them to go.mod
	go mod tidy

fmt: ## Format code
	go fmt ./...
	gofmt -s -w $(GO_FILES)

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

test: mod-tidy ## Run mod-tidy, then all tests with race detection
	go test -v -race ./...

generate: ## Run go generate across all packages
	go generate ./...
