.PHONY: build test lint run clean deps fmt vet install restart update
.PHONY: build-all build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64

# Binary name
BINARY_NAME=pako-telegram
BUILD_DIR=bin
CONFIG_DIR=$(HOME)/.config/pako-telegram
INSTALL_DIR=$(HOME)/.config/pako-telegram/bin
SERVICE_NAME=com.pako-telegram

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet
GOMOD=$(GOCMD) mod

# Version info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -s -w \
	-X github.com/rashpile/pako-telegram/internal/version.Version=$(VERSION) \
	-X github.com/rashpile/pako-telegram/internal/version.Commit=$(COMMIT) \
	-X github.com/rashpile/pako-telegram/internal/version.BuildDate=$(BUILD_DATE)

# Build the application
build:
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/pako-telegram

# Run tests
test:
	$(GOTEST) -v -race ./...

# Run tests with coverage
test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

# Format code
fmt:
	$(GOFMT) ./...

# Vet code
vet:
	$(GOVET) ./...

# Run the application
run:
	$(GOCMD) run ./cmd/pako-telegram

# Run with hot reload (requires air)
dev:
	air

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Download dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Install development tools
install-tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/air-verse/air@latest

# Cross-platform builds
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64

build-linux-amd64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/$(BINARY_NAME)

build-linux-arm64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/$(BINARY_NAME)

build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/$(BINARY_NAME)

build-darwin-arm64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/$(BINARY_NAME)

build-windows-amd64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/$(BINARY_NAME)

# Alias for backward compatibility
build-linux: build-linux-amd64

# Docker build
docker-build:
	docker build -t $(BINARY_NAME):latest .

# Docker run
docker-run:
	docker run -p 8080:8080 $(BINARY_NAME):latest

# All checks before commit
check: fmt vet lint test

# Install binary and commands locally, restart service
install: build
	@echo "Installing binary to $(INSTALL_DIR)..."
	@mkdir -p $(INSTALL_DIR)
	cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Copying commands to $(CONFIG_DIR)/commands..."
	@mkdir -p $(CONFIG_DIR)/commands
	cp $(BUILD_DIR)/commands/*.yaml $(CONFIG_DIR)/commands/
	@echo "Restarting service..."
	@launchctl stop $(SERVICE_NAME) 2>/dev/null || true
	@launchctl start $(SERVICE_NAME)
	@echo "Done! Checking logs..."
	@sleep 1
	@tail -5 $(CONFIG_DIR)/stdout.log

# Restart the service without rebuilding
restart:
	@launchctl stop $(SERVICE_NAME) 2>/dev/null || true
	@launchctl start $(SERVICE_NAME)
	@echo "Service restarted"

# GitHub release info
GITHUB_REPO=rashpile/pako-telegram
OS=$(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

# Update binary from latest GitHub release
update:
	@echo "Fetching latest release..."
	$(eval LATEST_VERSION := $(shell curl -s https://api.github.com/repos/$(GITHUB_REPO)/releases/latest | grep '"tag_name"' | cut -d'"' -f4))
	@if [ -z "$(LATEST_VERSION)" ]; then echo "Error: Could not fetch latest version"; exit 1; fi
	@echo "Latest version: $(LATEST_VERSION)"
	@echo "Downloading $(BINARY_NAME)-$(OS)-$(ARCH)..."
	$(eval TEMP_FILE := $(shell mktemp))
	@curl -L -o $(TEMP_FILE) https://github.com/$(GITHUB_REPO)/releases/download/$(LATEST_VERSION)/$(BINARY_NAME)-$(OS)-$(ARCH)
	@chmod +x $(TEMP_FILE)
	@echo "Installing to $(INSTALL_DIR)/$(BINARY_NAME)..."
	@mkdir -p $(INSTALL_DIR)
	@mv $(TEMP_FILE) $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Updated to $(LATEST_VERSION)"
