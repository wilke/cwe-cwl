# Makefile for CWL Workflow Engine

.PHONY: all build test clean fmt lint deps server scheduler cli

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go variables
GO := go
GOFLAGS := -v

# Binary names
SERVER_BINARY := cwe-server
SCHEDULER_BINARY := cwe-scheduler
CLI_BINARY := cwe-cli

# Directories
BIN_DIR := bin
CMD_DIR := cmd

all: build

# Build all binaries
build: server scheduler cli

# Build the API server
server:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(SERVER_BINARY) ./$(CMD_DIR)/cwe-server

# Build the scheduler daemon
scheduler:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(SCHEDULER_BINARY) ./$(CMD_DIR)/cwe-scheduler

# Build the CLI
cli:
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(CLI_BINARY) ./$(CMD_DIR)/cwe-cli

# Install dependencies
deps:
	$(GO) mod download
	$(GO) mod verify

# Run tests
test:
	$(GO) test -v -race -cover ./...

# Run tests with coverage report
test-coverage:
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	$(GO) fmt ./...
	gofmt -s -w .

# Run linter
lint:
	golangci-lint run ./...

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	rm -f coverage.out coverage.html

# Run the server (development)
run-server: server
	./$(BIN_DIR)/$(SERVER_BINARY) -config configs/config.dev.yaml

# Run the scheduler (development)
run-scheduler: scheduler
	./$(BIN_DIR)/$(SCHEDULER_BINARY) -config configs/config.dev.yaml

# Docker build
docker-build:
	docker build -t cwe-cwl:$(VERSION) .

# Generate API documentation
docs:
	swag init -g cmd/cwe-server/main.go -o docs

# Install development tools
tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/swaggo/swag/cmd/swag@latest
