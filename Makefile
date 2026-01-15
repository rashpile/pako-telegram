.PHONY: build test lint run clean deps fmt vet install restart

# Binary name
BINARY_NAME=pako-telegram
BUILD_DIR=bin
CONFIG_DIR=$(HOME)/.config/pako-telegram
INSTALL_DIR=/usr/local/bin
SERVICE_NAME=com.pako-telegram

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet
GOMOD=$(GOCMD) mod

# Build the application
build:
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/pako-telegram

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

# Build for Docker
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/pako-telegram

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
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
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
