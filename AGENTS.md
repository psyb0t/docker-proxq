# proxq

Instructions for coding agents (Claude Code, Codex and others) and people changing this repository. Users of the image want the [documentation site](https://psyb0t.github.io/docker-proxq/) instead.

## Agent setup

`CLAUDE.md` is a committed symlink to `AGENTS.md`, and `.claude` is a committed symlink to `.agents`, so Claude Code reads the same instructions, rules and skills as Codex. Claude's Edit and Write tools refuse to write through a symlink, so edit `AGENTS.md` and `.agents/rules/*.md` directly.

## What it is

A Docker image, `psyb0t/proxq`, holding one Go binary that turns HTTP requests into jobs in a Redis queue. A client sends any request, gets `202 Accepted` with a job ID, and later reads the job status and the stored upstream response through a small jobs API (`/__jobs` by default). WebSocket upgrades, chunked bodies, large bodies and paths picked by a per-upstream filter skip the queue and go straight to the upstream. Responses can be cached in memory or in Redis.

The module also ships a public Go package, `pkg/clients/openai`, that wraps the openai-go SDK so its calls go through proxq.

## Architecture

- `cmd/main.go`: resolves the config path (`--config`, then `PROXQ_CONFIG`, then `config.yaml`) and calls `app.Run`. The blank import of `slogging/slogconf` configures logging from `LOG_LEVEL`, `LOG_FORMAT` and `LOG_ADD_SOURCE`.
- `internal/config`: YAML parsing, defaults, validation and sentinel errors. `Parse` applies defaults, validates, compiles the path filter regexes, then sorts upstreams longest prefix first.
- `internal/app`: wiring. One process runs both the asynq worker server and the HTTP server (aichteeteapee `serbewr`) on the same Redis, and builds the cache (common-go `cache`).
- `internal/proxy/handler.go`: the catch-all route. Resolves the upstream, decides between direct proxy (WebSocket, chunked, size threshold, path filter) and enqueueing, and writes the `202` with the job ID.
- `internal/proxy/worker.go`: the asynq task handler. Forwards the stored request with aichteeteapee `prawxxey.ForwardRequest` (cache, hop-by-hop header stripping, `X-Forwarded-*`) and writes the response as the task result.
- `internal/proxy/jobs_handler.go`: `GET {jobsPath}/{id}`, `GET {jobsPath}/{id}/content`, `DELETE {jobsPath}/{id}` over an asynq `Inspector`.
- `internal/proxy/job.go`: task type name, status mapping from asynq states, the task envelope and the retry delay function.
- `pkg/types`: public header names (`X-Proxq-Source`, `X-Proxq-Timeout`) shared by the server and the client.
- `pkg/clients/openai`: an `http.RoundTripper` that enqueues, polls and fetches content, returned inside a real `*openai.Client`.

Data flow: client -> serbewr router (request ID, logger, recovery) -> `Handler.ServeHTTP` -> asynq task in Redis -> `Worker.ProcessTask` -> upstream -> task result in Redis -> `JobsHandler.Content` replays it.

## Stack

- Language: Go, `go 1.26.9` in `go.mod`, dependencies vendored in `vendor/`.
- Libraries: asynq (queue), go-redis, aichteeteapee (`serbewr` server, `prawxxey` forwarding), common-go (`cache`, `slogging`, `errors`), ctxerrors, yaml.v3, openai-go v3 (client package only).
- Image: `Dockerfile` builds a static binary on `golang:1.26.10-alpine` and runs it on `alpine:3.24` as the non-root user `proxq`, both pinned by digest.
- Tests: testify, testcontainers-go (Redis), `-race`.
- Lint: golangci-lint v2 run through `go tool` (pinned in the `tool` block of `go.mod`), config in `.golangci.yml`.

## Layout

| Path | Holds |
| --- | --- |
| `cmd/` | `main.go`, config path resolution |
| `internal/app/` | process wiring: asynq client, inspector and server, cache, HTTP routes |
| `internal/config/` | config struct, defaults, validation, sentinel errors |
| `internal/proxy/` | request handler, worker, jobs API, job types |
| `internal/testinfra/` | Redis testcontainer helpers for integration tests |
| `pkg/types/` | public header constants |
| `pkg/clients/openai/` | the OpenAI SDK client that goes through proxq |
| `tests/` | separate Go module with end-to-end tests that build the image (not run by `make test` or CI) |
| `config.yaml.example` | example config with every field |
| `docs/` | the documentation site, see below |
| `.agents/` | the agent skill (`skills/proxq/`), the Claude and Codex plugin manifests, and these rules |
| `.github/workflows/pipeline.yml` | CI: lint, tests, govulncheck, image build, tag promotion, release, ClawHub |

`docs/` is the documentation site, published by GitHub Pages from `main` at https://psyb0t.github.io/docker-proxq/ (Jekyll with the just-the-docs theme).

## Commands

Docker, Make and Bash are enough. Every Go command runs in a pinned `golang` container, with build caches in `.testing/go-cache`.

```bash
make help            # every target
make lint            # go fix -diff and golangci-lint
make lint-fix        # apply go fix and golangci-lint fixes
make test            # go test -race ./..., integration tests start Redis through testcontainers
make test-coverage   # tests with the 90% coverage gate, writes coverage-percent.txt
make sec             # govulncheck
make dep             # go mod tidy and go mod vendor
make build           # docker build -t psyb0t/proxq:latest .
make docs            # build the documentation site into .testing/docs-site
```

`make test` and `make test-coverage` mount the Docker socket into the Go container and use host networking, because the integration tests start Redis with testcontainers.

## Rules

Rules for every change: `.agents/rules/`. Read them before editing.
