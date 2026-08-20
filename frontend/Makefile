# agentapi-ui Makefile
# Development build and deployment automation

.PHONY: help install build lint typecheck test dev clean \
        docker-build docker-push \
        devbuild devbuild-image devbuild-helm

# Variables
IMAGE_NAME := agentapi-ui
REGISTRY := ghcr.io/takutakahashi
FULL_IMAGE_NAME := $(REGISTRY)/$(IMAGE_NAME)
GH_REPO := $(shell git config --get remote.origin.url | sed 's/.*github.com[:/]\(.*\)\.git/\1/' | sed 's/.*github.com[:/]\(.*\)/\1/')
GIT_REF := $(shell git rev-parse --abbrev-ref HEAD)
GIT_SHA := $(shell git rev-parse --short HEAD)

# Colors for output
CYAN := \033[36m
GREEN := \033[32m
YELLOW := \033[33m
RESET := \033[0m

##@ General

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make $(CYAN)<target>$(RESET)\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  $(CYAN)%-20s$(RESET) %s\n", $$1, $$2 } /^##@/ { printf "\n$(YELLOW)%s$(RESET)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

install: ## Install dependencies
	@echo "$(GREEN)Installing dependencies...$(RESET)"
	bun install

build: ## Build the Next.js application
	@echo "$(GREEN)Building application...$(RESET)"
	bun run build

lint: ## Run ESLint
	@echo "$(GREEN)Running lint...$(RESET)"
	bun run lint

typecheck: ## Run TypeScript type checking
	@echo "$(GREEN)Running type check...$(RESET)"
	npx tsc --noEmit

test: ## Run tests
	@echo "$(GREEN)Running tests...$(RESET)"
	bun run test

dev: ## Start development server
	@echo "$(GREEN)Starting development server...$(RESET)"
	bun run dev

clean: ## Clean build artifacts
	@echo "$(GREEN)Cleaning build artifacts...$(RESET)"
	rm -rf .next out node_modules/.cache

##@ Docker

docker-build: ## Build Docker image locally
	@echo "$(GREEN)Building Docker image $(FULL_IMAGE_NAME):$(GIT_SHA)...$(RESET)"
	docker build -t $(FULL_IMAGE_NAME):$(GIT_SHA) -t $(FULL_IMAGE_NAME):dev .

docker-push: docker-build ## Build and push Docker image
	@echo "$(GREEN)Pushing Docker image $(FULL_IMAGE_NAME)...$(RESET)"
	docker push $(FULL_IMAGE_NAME):$(GIT_SHA)
	docker push $(FULL_IMAGE_NAME):dev

##@ CI/CD - Development Builds

devbuild: ## Trigger full devbuild (image + helm chart) via GitHub Actions
	@echo "$(GREEN)Triggering development build workflow...$(RESET)"
	@echo "Repository: $(GH_REPO)"
	@echo "Git Ref: $(GIT_REF)"
	gh workflow run helm-dev-build.yml \
		--repo $(GH_REPO) \
		--ref $(GIT_REF) \
		-f git_ref=$(GIT_REF) \
		-f dry_run=false
	@echo "$(GREEN)Workflow dispatched. Check GitHub Actions for progress.$(RESET)"

devbuild-image: ## Trigger development image build via GitHub Actions
	@echo "$(GREEN)Triggering development image build workflow...$(RESET)"
	@echo "Repository: $(GH_REPO)"
	@echo "Git Ref: $(GIT_REF)"
	gh workflow run docker-dev.yaml \
		--repo $(GH_REPO) \
		--ref $(GIT_REF) \
		-f tag=dev
	@echo "$(GREEN)Workflow dispatched. Check GitHub Actions for progress.$(RESET)"

devbuild-helm: ## Trigger development helm chart build via GitHub Actions
	@echo "$(GREEN)Triggering development helm chart build workflow...$(RESET)"
	@echo "Repository: $(GH_REPO)"
	@echo "Git Ref: $(GIT_REF)"
	gh workflow run helm-dev-build.yml \
		--repo $(GH_REPO) \
		--ref $(GIT_REF) \
		-f git_ref=$(GIT_REF) \
		-f dry_run=false \
		-f skip_image_build=true
	@echo "$(GREEN)Workflow dispatched. Check GitHub Actions for progress.$(RESET)"

##@ Helm

helm-lint: ## Lint Helm chart
	@echo "$(GREEN)Linting Helm chart...$(RESET)"
	helm lint helm/agentapi-ui --strict

helm-template: ## Render Helm chart templates
	@echo "$(GREEN)Rendering Helm templates...$(RESET)"
	helm template agentapi-ui helm/agentapi-ui

helm-package: ## Package Helm chart
	@echo "$(GREEN)Packaging Helm chart...$(RESET)"
	helm package helm/agentapi-ui --destination ./charts

##@ Quality Assurance

ci: lint typecheck build ## Run full CI pipeline locally
	@echo "$(GREEN)CI pipeline completed successfully!$(RESET)"
