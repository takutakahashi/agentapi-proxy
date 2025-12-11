.PHONY: help install-deps build test lint clean docker-build docker-push e2e ci gofmt setup-envtest envtest devbuild devbuild-image devbuild-helm

BINARY_NAME := agentapi-proxy
GO_FILES := $(shell find . -name "*.go" -type f)
IMAGE_NAME := agentapi-proxy
IMAGE_TAG := latest
REGISTRY ?= ghcr.io/takutakahashi
FULL_IMAGE_NAME := $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# envtest settings
ENVTEST_K8S_VERSION ?= 1.29.0
LOCALBIN ?= $(shell pwd)/bin
ENVTEST ?= $(LOCALBIN)/setup-envtest

help:
	@echo "Available targets:"
	@echo "  install-deps  - Install project dependencies"
	@echo "  build         - Build the Go binary"
	@echo "  test          - Run Go tests"
	@echo "  lint          - Run linters (golangci-lint)"
	@echo "  gofmt         - Format Go code with gofmt -s -w"
	@echo "  clean         - Clean build artifacts"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image to registry"
	@echo "  e2e           - Run end-to-end tests"
	@echo "  ci            - Run CI pipeline (lint, test, build)"
	@echo "  setup-envtest - Install setup-envtest tool"
	@echo "  envtest       - Run Kubernetes envtest tests"
	@echo "  devbuild      - Build dev image and helm chart (dispatch GitHub workflow)"
	@echo "  devbuild-image - Build dev image only (dispatch GitHub workflow)"
	@echo "  devbuild-helm - Build dev helm chart only (dispatch GitHub workflow)"

install-deps:
	@echo "Installing project dependencies..."
	@echo "Installing Go modules..."
	go mod download
	@echo "Installing golangci-lint v2..."
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found, installing..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v2.7.2; \
	}
	@echo "Dependencies installed successfully"

build:
	@echo "Building $(BINARY_NAME)..."
	go mod tidy
	go build -o bin/$(BINARY_NAME) main.go

gofmt:
	@echo "Formatting Go code..."
	go fmt ./...

test: gofmt setup-envtest
	@echo "Running tests..."
	go test -v -race ./...
	@echo "Running envtest tests..."
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test -v -race -tags=envtest ./pkg/proxy/... -run "^TestKubernetes"

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
		echo "Running e2e tests directly..."; \
		go test -v -timeout=${GO_TEST_TIMEOUT:-60s} ./test/... -tags=e2e; \
	fi

ci: lint test build
	@echo "CI pipeline completed successfully"

# setup-envtest: Install the setup-envtest tool
setup-envtest: $(ENVTEST)
$(ENVTEST): $(LOCALBIN)
	@echo "Installing setup-envtest..."
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# envtest: Run Kubernetes envtest tests
envtest: setup-envtest
	@echo "Running envtest tests..."
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		go test -v -race -tags=envtest ./pkg/proxy/... -run "^Test(Kubernetes|Sanitize)"

# GitHub repository for workflow dispatch
GITHUB_REPO ?= $(shell git config --get remote.origin.url | sed -E 's|.*github.com[:/](.*)\.git|\1|')
GIT_REF ?= $(shell git rev-parse --abbrev-ref HEAD)

# devbuild: Build dev image and helm chart (triggers both workflows)
devbuild:
	@echo "Dispatching Development Helm Chart build workflow..."
	@echo "Repository: $(GITHUB_REPO)"
	@echo "Git Reference: $(GIT_REF)"
	@gh workflow run "Build Development Helm Chart" \
		--repo $(GITHUB_REPO) \
		--ref $(GIT_REF) \
		-f git_ref=$(GIT_REF)
	@echo ""
	@echo "✅ Workflow dispatched successfully!"
	@echo "This will build both the Docker image and Helm chart."
	@echo ""
	@echo "View progress at: https://github.com/$(GITHUB_REPO)/actions"

# devbuild-image: Build dev image only
devbuild-image:
	@echo "Dispatching Development Image build workflow..."
	@echo "Repository: $(GITHUB_REPO)"
	@echo "Git Reference: $(GIT_REF)"
	@gh workflow run "Build Development Image" \
		--repo $(GITHUB_REPO) \
		--ref $(GIT_REF) \
		-f git_ref=$(GIT_REF) \
		-f platforms="linux/amd64"
	@echo ""
	@echo "✅ Workflow dispatched successfully!"
	@echo ""
	@echo "View progress at: https://github.com/$(GITHUB_REPO)/actions"

# devbuild-helm: Build dev helm chart only (assumes image already exists)
devbuild-helm:
	@echo "Dispatching Development Helm Chart build workflow (dry-run for image)..."
	@echo "Repository: $(GITHUB_REPO)"
	@echo "Git Reference: $(GIT_REF)"
	@gh workflow run "Build Development Helm Chart" \
		--repo $(GITHUB_REPO) \
		--ref $(GIT_REF) \
		-f git_ref=$(GIT_REF) \
		-f dry_run=false
	@echo ""
	@echo "✅ Workflow dispatched successfully!"
	@echo ""
	@echo "View progress at: https://github.com/$(GITHUB_REPO)/actions"
