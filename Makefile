IMAGE_NAME       := psyb0t/proxq
TAG              := latest
TEST_TAG         := $(TAG)-test
MIN_TEST_COVERAGE := 90

SHELL := /bin/bash

# Every Go command runs in this image, so the host needs only Docker, Make and
# Bash. It is the Debian variant of the Dockerfile's builder Go version,
# because -race needs cgo and a C compiler. Pinned by digest; bump it together
# with the Dockerfile.
GO_IMAGE := golang:1.26.10-trixie@sha256:fd9a91f0ece5664556f4851d06dce5e5b505a71166dffd385a03ba9d200b6983

# Build and module caches live in the gitignored .testing/ directory so
# repeated runs stay fast without writing outside the checkout.
GO_CACHE_DIR := $(CURDIR)/.testing/go-cache
DOCKER_SOCK  := /var/run/docker.sock
DOCKER_GID   := $(shell stat -c '%g' $(DOCKER_SOCK) 2>/dev/null || echo 0)

GO_RUN_BASE = docker run --rm --init \
	--user "$$(id -u):$$(id -g)" \
	-e HOME=/tmp \
	-e GOPATH=$(GO_CACHE_DIR)/gopath \
	-e GOCACHE=$(GO_CACHE_DIR)/build \
	-e GOLANGCI_LINT_CACHE=$(GO_CACHE_DIR)/golangci-lint \
	-e GOTOOLCHAIN=local \
	-v "$(CURDIR):$(CURDIR)" \
	-w "$(CURDIR)"

# Plain Go container for lint and builds.
GO_RUN = $(GO_RUN_BASE) $(GO_IMAGE)

# Go container with the Docker socket, for the integration tests that start
# Redis through testcontainers. Host networking makes the ports testcontainers
# maps on the Docker host reachable on localhost.
GO_RUN_DIND = $(GO_RUN_BASE) \
	--group-add $(DOCKER_GID) \
	--network host \
	-e TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=$(DOCKER_SOCK) \
	-e TESTCONTAINERS_RYUK_DISABLED=true \
	-v "$(DOCKER_SOCK):$(DOCKER_SOCK)" \
	$(GO_IMAGE)

GOVULNCHECK_VERSION := v1.3.0

DOCS_SITE ?= .testing/docs-site

.PHONY: all build build-test dep lint lint-fix test test-coverage sec docs clean help go-cache

all: dep lint test ## Run dep, lint and test

go-cache:
	@mkdir -p "$(GO_CACHE_DIR)"

dep: go-cache ## Download, tidy and vendor dependencies
	@echo "Getting project dependencies..."
	@$(GO_RUN) sh -ceu 'go mod tidy && go mod vendor'

lint: go-cache ## Lint all Go files (go fix -diff, golangci-lint)
	@echo "Linting all Go files..."
	@$(GO_RUN) sh -ceu ' \
		out=$$(go fix -diff ./... 2>&1); \
		if [ -n "$$out" ]; then \
			echo "$$out"; \
			echo "go fix found issues. Run make lint-fix to apply."; \
			exit 1; \
		fi; \
		go tool golangci-lint run --timeout=30m0s ./...'

lint-fix: go-cache ## Apply go fix and golangci-lint fixes
	@echo "Linting and fixing all Go files..."
	@$(GO_RUN) sh -ceu 'go fix ./... && go tool golangci-lint run --fix --timeout=30m0s ./...'

test: go-cache ## Run unit and integration tests with the race detector
	@echo "Running tests..."
	@$(GO_RUN_DIND) go test -race ./...

test-coverage: go-cache ## Run tests with coverage check. Fails if coverage is below the threshold.
	@echo "Running tests with coverage check..."
	@trap 'rm -f coverage.txt' EXIT; \
	packages=$$($(GO_RUN) go list ./... | grep -v /internal/app | grep -v /internal/testinfra | grep -v /cmd | grep -v /tests); \
	if ! $(GO_RUN_DIND) go test -race -coverprofile=coverage.txt $$packages; then \
		echo "Test failed. Exiting."; \
		exit 1; \
	fi; \
	summary=$$($(GO_RUN) go tool cover -func=coverage.txt); \
	result=$$(echo "$$summary" | grep -oP 'total:\s+\(statements\)\s+\K\d+' || echo "0"); \
	pct=$$(echo "$$summary" | grep -oP 'total:\s+\(statements\)\s+\K[0-9.]+' || echo "0"); \
	echo "$$pct" > coverage-percent.txt; \
	if [ $$result -eq 0 ]; then \
		echo "No test coverage information available."; \
		exit 0; \
	elif [ $$result -lt $(MIN_TEST_COVERAGE) ]; then \
		echo "FAIL: Coverage $$result% is less than the minimum $(MIN_TEST_COVERAGE)%"; \
		exit 1; \
	fi

sec: go-cache ## Scan dependencies and the standard library with govulncheck
	@echo "Running govulncheck..."
	@$(GO_RUN) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Builds docs/ with the image GitHub Pages uses, so the local result matches
# the published site. The output directory is gitignored.
docs: ## Build the documentation site into .testing/docs-site
	@mkdir -p "$(DOCS_SITE)"
	@docker run --rm \
		--user "$$(id -u):$$(id -g)" \
		-e HOME=/tmp \
		-e GITHUB_WORKSPACE=/work \
		-e GITHUB_REPOSITORY=psyb0t/docker-proxq \
		-e INPUT_SOURCE=docs \
		-e INPUT_DESTINATION="$(DOCS_SITE)" \
		-e INPUT_VERBOSE=false \
		-v "$(CURDIR):/work" \
		ghcr.io/actions/jekyll-build-pages:v1.0.13@sha256:6791ebfd912185ed59bfb5fb102664fa872496b79f87ff8b9cfba292a7345041

build: ## Build the Docker image
	docker build -t $(IMAGE_NAME):$(TAG) .

build-test: ## Build the test Docker image
	docker build -t $(IMAGE_NAME):$(TEST_TAG) .

clean: ## Remove built images
	docker rmi $(IMAGE_NAME):$(TAG) || true
	docker rmi $(IMAGE_NAME):$(TEST_TAG) || true

help: ## Display this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
