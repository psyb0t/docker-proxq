IMAGE_NAME := psyb0t/proxq
TAG        := latest
TEST_TAG   := $(TAG)-test

.PHONY: all build build-test dep lint lint-fix test clean help

all: dep lint test ## Run dep, lint and test

dep: ## Download and tidy dependencies
	@echo "Getting project dependencies..."
	@go mod tidy
	@go mod vendor

lint: ## Lint all Go files
	@echo "Linting all Go files..."
	@go fix ./...
	@go tool golangci-lint run --timeout=30m0s ./...

lint-fix: ## Lint and auto-fix
	@echo "Linting and fixing Go files..."
	@go fix ./...
	@go tool golangci-lint run --fix --timeout=30m0s ./...

test: ## Run tests
	@echo "Running tests..."
	@go test -race ./...

build: ## Build the Docker image
	docker build -t $(IMAGE_NAME):$(TAG) .

build-test: ## Build the test Docker image
	docker build -t $(IMAGE_NAME):$(TEST_TAG) .

clean: ## Remove built images
	docker rmi $(IMAGE_NAME):$(TAG) || true
	docker rmi $(IMAGE_NAME):$(TEST_TAG) || true

help: ## Display this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
