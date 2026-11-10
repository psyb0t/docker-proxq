---
title: "Development"
description: "Build, lint and test proxq from source with Make and Docker, run the end-to-end suite, build these docs, and how the CI pipeline releases."
nav_order: 12
---

# Development

Docker, Make and Bash are all you need on the host. Every Go command runs in a pinned `golang` container, and build caches go to the gitignored `.testing/go-cache` directory.

```bash
make help            # every target
make lint            # go fix -diff and golangci-lint
make lint-fix        # apply go fix and golangci-lint fixes
make test            # unit and integration tests, race detector on
make test-coverage   # tests with the 90% coverage gate
make sec             # govulncheck
make dep             # go mod tidy and go mod vendor
make build           # docker build -t psyb0t/proxq:latest .
make docs            # build this documentation site into .testing/docs-site
```

Dependencies are vendored in `vendor/`. After changing imports, run `make dep` and commit `go.mod`, `go.sum` and `vendor/` together. New imports also need an entry in the `depguard` allow list in [`.golangci.yml`](https://github.com/psyb0t/docker-proxq/blob/main/.golangci.yml).

## Layout

| Path | Holds |
| --- | --- |
| [`cmd/`](https://github.com/psyb0t/docker-proxq/tree/main/cmd) | `main.go` |
| [`internal/app/`](https://github.com/psyb0t/docker-proxq/tree/main/internal/app) | Wiring: asynq, HTTP server, cache |
| [`internal/config/`](https://github.com/psyb0t/docker-proxq/tree/main/internal/config) | YAML parsing, validation, defaults |
| [`internal/proxy/`](https://github.com/psyb0t/docker-proxq/tree/main/internal/proxy) | Request handler, worker, job types, jobs API |
| [`internal/testinfra/`](https://github.com/psyb0t/docker-proxq/tree/main/internal/testinfra) | Redis testcontainer helpers |
| [`pkg/types/`](https://github.com/psyb0t/docker-proxq/tree/main/pkg/types) | Public header constants |
| [`pkg/clients/openai/`](https://github.com/psyb0t/docker-proxq/tree/main/pkg/clients/openai) | The OpenAI SDK client |
| [`tests/`](https://github.com/psyb0t/docker-proxq/tree/main/tests) | End-to-end tests, a separate Go module |
| [`config.yaml.example`](https://github.com/psyb0t/docker-proxq/blob/main/config.yaml.example) | Example config with every field |
| `docs/` | This site |

## Tests

Unit tests sit beside the code. The `*_integration_test.go` files start a real Redis with testcontainers through `internal/testinfra`, including one with the `default` user disabled and a restricted ACL user. They run as part of `make test`, which mounts the Docker socket into the Go container and uses host networking so the container can reach the Redis ports.

`make test-coverage` runs the tests over every package except `internal/app`, `internal/testinfra`, `cmd` and `tests`, writes the total to `coverage-percent.txt`, and fails below 90%.

The `tests/` module holds end-to-end tests. They build the image, then start Redis, a Node.js echo upstream and proxq with testcontainers. They need Go on the host and are not part of CI:

```bash
cd tests
go test -v -timeout 10m ./...
```

Some of these tests still call an older signature of the `setup` helper in `tests/helpers_test.go`, so the module does not compile until they are updated.

## Documentation

This site is the `docs/` directory, built by GitHub Pages with Jekyll and the just-the-docs theme. `make docs` builds it with the same image GitHub uses. Every page needs front matter with a unique `title` and a `description` of 50 to 170 characters. Link to other pages with their relative `.md` path, and to repository files outside `docs/` with the full GitHub URL.

## CI

[`.github/workflows/pipeline.yml`](https://github.com/psyb0t/docker-proxq/blob/main/.github/workflows/pipeline.yml) runs on every push:

| Job | Runs on | Does |
| --- | --- | --- |
| `code` | every branch push | `make lint`, `make test-coverage`, `make sec` |
| `call-docker-image-workflow` | after `code` | On `main`: builds `linux/amd64` and `linux/arm64`, pushes `psyb0t/proxq:latest` and `:sha-<commit>`, scans with Grype |
| `badges` | `main` and tags | Coverage, version and license badges |
| `wait` | `v*` tags | Waits for the `main` run of the tagged commit to succeed |
| `docker-promote` | after `wait` | Points the release tag at the `:sha-<commit>` image, no rebuild |
| `release` | after `docker-promote` | Creates the GitHub release |
| `publish-to-clawhub` | after `release` | Publishes the `proxq` skill to ClawHub |

The runner needs only Docker, Make and Bash, because every target runs its tools in containers. Grype findings go to the Security tab without failing the run. A govulncheck finding fails `code`, which stops the image build.

To release, tag a commit that already passed on `main`. The tag pipeline does not rebuild or retest.
