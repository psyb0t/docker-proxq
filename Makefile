IMAGE_NAME       := psyb0t/proxq
TAG              := latest
TEST_TAG         := $(TAG)-test
MIN_TEST_COVERAGE := 90

SHELL := /bin/bash

# Every Go command runs in this image, so the host needs only Docker, Make and
# Bash. It is the Debian variant of the Dockerfile's builder Go version,
# because -race needs cgo and a C compiler. Pinned by digest; bump it together
# with the Dockerfile.
GO_IMAGE := golang:1.26.11-trixie@sha256:f9e99477a80ccd7adfb69780510b22c7249ce9169885f18aa762cd886e75e018

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

UID := $(shell id -u)
GID := $(shell id -g)
DOCS_NODE_IMAGE := node:24.21.0-bookworm-slim@sha256:d6aa754f16b3197301076f047b5def2f02ea1dbbc2ca920407d46d7ec7f87b20
DOCS_CACHE := .testing/docs-cache
DOCS_RUN := docker run --rm --init --cap-drop ALL --security-opt no-new-privileges:true \
	--user $(UID):$(GID) -e HOME=/tmp -e COREPACK_HOME=/cache/corepack \
	-v "$(CURDIR)/$(DOCS_CACHE):/cache" \
	-v "$(CURDIR)/docs:/work/docs:ro" -v "$(CURDIR)/website:/work/website" -w /work/website

.PHONY: all build build-test dep lint lint-fix test test-coverage sec clean help go-cache

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

.PHONY: docs docs-build docs-lock
docs: docs-build ## Build the documentation site into website/build, with the link check

docs-build: | $(DOCS_CACHE)
	$(DOCS_RUN) $(DOCS_NODE_IMAGE) corepack pnpm install --frozen-lockfile --ignore-scripts --store-dir /cache/pnpm
	$(DOCS_RUN) --network none $(DOCS_NODE_IMAGE) corepack pnpm run build

docs-lock: | $(DOCS_CACHE) ## Refresh website/pnpm-lock.yaml under the dependency age gate
	$(DOCS_RUN) $(DOCS_NODE_IMAGE) corepack pnpm install --lockfile-only --ignore-scripts --store-dir /cache/pnpm

$(DOCS_CACHE):
	mkdir -p "$@"

build: ## Build the Docker image
	docker build -t $(IMAGE_NAME):$(TAG) .

build-test: ## Build the test Docker image
	docker build -t $(IMAGE_NAME):$(TEST_TAG) .

clean: ## Remove built images
	docker rmi $(IMAGE_NAME):$(TAG) || true
	docker rmi $(IMAGE_NAME):$(TEST_TAG) || true

help: ## Display this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
