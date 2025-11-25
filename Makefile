.PHONY: help install-deps build test test-template test-integration test-all lint clean docker-build docker-push e2e ci gofmt setup-envtest

BINARY_NAME := agentapi-proxy
GO_FILES := $(shell find . -name "*.go" -type f)
IMAGE_NAME := agentapi-proxy
IMAGE_TAG := latest
REGISTRY ?= ghcr.io/takutakahashi
FULL_IMAGE_NAME := $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# envtest binary configuration
ENVTEST_K8S_VERSION = 1.30.0
ENVTEST_ASSETS_DIR = testbin/k8s
ENVTEST_BINARY_DIR = $(ENVTEST_ASSETS_DIR)/k8s/$(ENVTEST_K8S_VERSION)-linux-amd64
ENVTEST_SETUP_URL = https://storage.googleapis.com/kubebuilder-tools/kubebuilder-tools-$(ENVTEST_K8S_VERSION)-linux-amd64.tar.gz

help:
	@echo "Available targets:"
	@echo "  install-deps - Install project dependencies"
	@echo "  build        - Build the Go binary"
	@echo "  test         - Run Go unit tests"
	@echo "  test-template - Run template generation tests"
	@echo "  test-integration - Run integration tests"
	@echo "  test-all     - Run all tests (unit, template, integration)"
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

setup-envtest:
	@echo "Setting up envtest binaries..."
	@if [ ! -d "$(ENVTEST_BINARY_DIR)" ] || [ ! -f "$(ENVTEST_BINARY_DIR)/etcd" ]; then \
		echo "Downloading Kubernetes $(ENVTEST_K8S_VERSION) binaries..."; \
		mkdir -p $(ENVTEST_ASSETS_DIR); \
		curl -L https://go.kubebuilder.io/test-tools/$(ENVTEST_K8S_VERSION)/linux/amd64 -o /tmp/envtest.tar.gz; \
		tar xzf /tmp/envtest.tar.gz -C $(ENVTEST_ASSETS_DIR); \
		mkdir -p $(ENVTEST_BINARY_DIR); \
		mv $(ENVTEST_ASSETS_DIR)/kubebuilder/bin/* $(ENVTEST_BINARY_DIR)/; \
		rm -rf $(ENVTEST_ASSETS_DIR)/kubebuilder; \
		rm /tmp/envtest.tar.gz; \
		echo "envtest binaries downloaded successfully"; \
	else \
		echo "envtest binaries already exist at $(ENVTEST_BINARY_DIR)"; \
	fi

build:
	@echo "Building $(BINARY_NAME)..."
	go mod tidy
	go build -o bin/$(BINARY_NAME) main.go

gofmt:
	@echo "Formatting Go code..."
	go fmt ./...

test: gofmt setup-envtest
	@echo "Running unit tests..."
	go test -v -race ./internal/... ./pkg/... -short

test-template: gofmt
	@echo "Running template generation tests..."
	@echo "Checking if envsubst is available..."
	@command -v envsubst >/dev/null 2>&1 || { echo "envsubst not found. Install gettext package."; exit 1; }
	go test -v -race ./internal/infrastructure/services/template_service_test.go ./internal/infrastructure/services/template_service.go

test-integration: gofmt setup-envtest
	@echo "Running integration tests..."
	@echo "Checking if envsubst is available..."
	@command -v envsubst >/dev/null 2>&1 || { echo "envsubst not found. Install gettext package."; exit 1; }
	go test -v -race ./test/k8s_template_integration_test.go -tags=integration

test-all: test test-template test-integration
	@echo "All tests completed successfully"

lint: gofmt
	@echo "Running linters..."
	golangci-lint run --timeout=5m

clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -rf testbin/
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
		echo "Running e2e tests directly..."; \
		go test -v -timeout=${GO_TEST_TIMEOUT:-60s} ./test/... -tags=e2e; \
	fi

ci: lint test build
	@echo "CI pipeline completed successfully"
