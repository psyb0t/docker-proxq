IMAGE_NAME       := psyb0t/proxq
TAG              := latest
TEST_TAG         := $(TAG)-test
MIN_TEST_COVERAGE := 90

.PHONY: all build build-test dep lint lint-fix test test-coverage clean help

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

SHELL := /bin/bash

test-coverage: ## Run tests with coverage check. Fails if coverage is below the threshold.
	@echo "Running tests with coverage check..."
	@trap 'rm -f coverage.txt' EXIT; \
	packages=$$(go list ./... | grep -v /internal/app | grep -v /internal/testinfra | grep -v /cmd | grep -v /tests); \
	go test -race -coverprofile=coverage.txt $$packages; \
	if [ $$? -ne 0 ]; then \
		echo "Test failed. Exiting."; \
		exit 1; \
	fi; \
	result=$$(go tool cover -func=coverage.txt | grep -oP 'total:\s+\(statements\)\s+\K\d+' || echo "0"); \
	if [ $$result -eq 0 ]; then \
		echo "No test coverage information available."; \
		exit 0; \
	elif [ $$result -lt $(MIN_TEST_COVERAGE) ]; then \
		echo "FAIL: Coverage $$result% is less than the minimum $(MIN_TEST_COVERAGE)%"; \
		exit 1; \
	fi

build: ## Build the Docker image
	docker build -t $(IMAGE_NAME):$(TAG) .

build-test: ## Build the test Docker image
	docker build -t $(IMAGE_NAME):$(TEST_TAG) .

clean: ## Remove built images
	docker rmi $(IMAGE_NAME):$(TAG) || true
	docker rmi $(IMAGE_NAME):$(TEST_TAG) || true

help: ## Display this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
