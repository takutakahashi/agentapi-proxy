.PHONY: help install-deps build test lint clean docker-build docker-push e2e ci gofmt

BINARY_NAME := agentapi-proxy
GO_FILES := $(shell find . -name "*.go" -type f)
IMAGE_NAME := agentapi-proxy
IMAGE_TAG := latest
REGISTRY ?= ghcr.io/takutakahashi
FULL_IMAGE_NAME := $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

help:
	@echo "Available targets:"
	@echo "  install-deps - Install project dependencies"
	@echo "  build        - Build the Go binary"
	@echo "  test         - Run Go tests"
	@echo "  lint         - Run linters (golangci-lint)"
	@echo "  gofmt        - Format Go code with gofmt -s -w"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-push  - Push Docker image to registry"
	@echo "  e2e          - Run end-to-end tests"
	@echo "  ci           - Run CI pipeline (lint, test, build)"

install-deps:
	@echo "Installing project dependencies..."
	@echo "Installing Go modules..."
	go mod download
	@echo "Installing golangci-lint..."
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found, installing..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin; \
	}
	@echo "Dependencies installed successfully"

build:
	@echo "Building $(BINARY_NAME)..."
	go mod tidy
	go build -o bin/$(BINARY_NAME) ./cmd/agentapi-proxy

gofmt:
	@echo "Formatting Go code..."
	gofmt -s -w $(GO_FILES)

test: gofmt
	@echo "Running tests..."
	go test -v -race ./...

lint: gofmt
	@echo "Running linters..."
	golangci-lint run --timeout=5m

clean:
	@echo "Cleaning up..."
	rm -rf bin/
	go clean

docker-build:
	@echo "Building Docker image $(FULL_IMAGE_NAME)..."
	docker build -t $(FULL_IMAGE_NAME) .

docker-push: docker-build
	@echo "Pushing Docker image $(FULL_IMAGE_NAME)..."
	docker push $(FULL_IMAGE_NAME)

e2e:
	@echo "Running end-to-end tests..."
	@if [ -f "test/e2e.sh" ]; then \
		bash test/e2e.sh; \
	else \
		echo "No e2e tests found. Create test/e2e.sh to run e2e tests."; \
	fi

ci: lint test build
	@echo "CI pipeline completed successfully"