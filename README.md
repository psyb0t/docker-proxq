# proxq

[![CI](https://github.com/psyb0t/docker-proxq/actions/workflows/pipeline.yml/badge.svg?branch=main)](https://github.com/psyb0t/docker-proxq/actions/workflows/pipeline.yml)
[![coverage](https://raw.githubusercontent.com/psyb0t/docker-proxq/badges/coverage.svg)](https://github.com/psyb0t/docker-proxq/actions/workflows/pipeline.yml)
[![version](https://raw.githubusercontent.com/psyb0t/docker-proxq/badges/version.svg)](https://github.com/psyb0t/docker-proxq/releases)
[![license](https://raw.githubusercontent.com/psyb0t/docker-proxq/badges/license.svg)](https://github.com/psyb0t/docker-proxq/blob/main/LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/psyb0t/proxq?style=flat-square)](https://hub.docker.com/r/psyb0t/proxq)

The honey badger of HTTP proxies. Takes your request, throws it in a Redis-backed job queue, and deals with it when it damn well pleases. You get a job ID back instantly, so come back later to pick up the goods.

Think of it as "I'll get back to you" as a service. Every HTTP request becomes an async job. No more hanging connections, no more timeouts, no more "please hold." Fire and forget, poll when ready.

Oh, and it caches too. Because hitting the same endpoint twice is for people who enjoy watching paint dry.

## Contents

- [How it works](#how-it-works)
- [Quick start](#quick-start)
- [Documentation](#documentation)
- [OpenAI Go client](#openai-go-client)
- [Agent integrations](#agent-integrations)
- [Development](#development)
- [Sponsor](#sponsor)
- [License](#license)

## How it works

```
Client           proxq            Redis          Upstream
 |                 |                 |               |
 |-- POST /foo --> |                 |               |
 |<- 202 {jobId} - |                 |               |
 |                 |-- enqueue ----> |               |
 |  (go touch      |                 |               |
 |   grass)        |                 | <- worker --- |
 |                 |                 |    wakes up   |
 |                 |                 | -----------> (request)
 |                 |                 | <----------- (response)
 |                 |                 |               |
 |-- GET /{jobId}->|                 |               |
 |<- {status} ---- |                 |               |
 |                 |                 |               |
 |-- GET /content->|                 |               |
 |<- {response} -- |                 |               |
 |                 |                 |               |
 |-- PUT /big ---> | --------- direct proxy ------>  |
 |<- {response} -- | <-----------------------------  |
```

proxq takes any request (any method, any path, any body), picks the upstream by longest path prefix, stores the whole thing in Redis through [asynq](https://github.com/hibiken/asynq) and hands you a job ID. A worker fires it at the upstream with your original headers plus `X-Forwarded-*`, stores the response, and you come back whenever you want to read the status and replay the response, status code, headers and body included.

Large uploads, chunked transfers, WebSocket connections and paths you pick with a filter skip the queue and are proxied straight to the upstream. No buffering, no double transfer, no memory bomb.

## Quick start

```yaml
services:
  proxq:
    image: psyb0t/proxq
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      PROXQ_CONFIG: /etc/proxq/config.yaml
    configs:
      - source: proxq_config
        target: /etc/proxq/config.yaml
    depends_on:
      - redis

  redis:
    image: redis:7-alpine
    restart: unless-stopped

configs:
  proxq_config:
    content: |
      listenAddress: "0.0.0.0:8080"
      redis:
        addr: "redis:6379"
      upstreams:
        - prefix: "/"
          url: "http://your-api:3000"
```

```bash
docker compose up -d

JOB_ID=$(curl -s -X POST localhost:8080/reports -d '{}' | jq -r .jobId)

curl -s "localhost:8080/__jobs/$JOB_ID"          # queued, running, completed or failed
curl -si "localhost:8080/__jobs/$JOB_ID/content" # the upstream response, replayed
```

That's it. Your API is now async. You're welcome.

proxq has no built-in authentication, so keep it on loopback or a private network, or put an authenticating reverse proxy in front. The [security page](https://psyb0t.github.io/docker-proxq/security/) has the details.

## Documentation

The full documentation is at [psyb0t.github.io/docker-proxq](https://psyb0t.github.io/docker-proxq/).

| Page | What it covers |
| --- | --- |
| [Configuration](https://psyb0t.github.io/docker-proxq/configuration/) | Every config field with its default, retries, validation, logging |
| [Jobs API](https://psyb0t.github.io/docker-proxq/api/) | Submit, status, content, cancel, and the `X-Proxq-Source` header |
| [Routing and bypass](https://psyb0t.github.io/docker-proxq/routing/) | Prefix matching and stripping, direct proxy rules, redirect mode |
| [Caching](https://psyb0t.github.io/docker-proxq/caching/) | Memory and Redis caches, the cache key |
| [OpenAI Go client](https://psyb0t.github.io/docker-proxq/openai-client/) | The openai-go wrapper |
| [Use cases](https://psyb0t.github.io/docker-proxq/use-cases/) | Example configs for common setups |
| [Agent integrations](https://psyb0t.github.io/docker-proxq/agents/) | The skill for Claude Code, Codex and OpenClaw |
| [Security](https://psyb0t.github.io/docker-proxq/security/) | What proxq exposes and what to put in front of it |
| [Troubleshooting](https://psyb0t.github.io/docker-proxq/troubleshooting/) | Startup errors and runtime problems |
| [Architecture](https://psyb0t.github.io/docker-proxq/architecture/) | Components, request path, job lifecycle |
| [Development](https://psyb0t.github.io/docker-proxq/development/) | Building, testing and CI |

[`config.yaml.example`](https://github.com/psyb0t/docker-proxq/blob/main/config.yaml.example) lists every config field.

## OpenAI Go client

Drop-in replacement for the [`openai-go`](https://github.com/openai/openai-go) constructor. Swap one line and your SDK calls go through proxq:

```go
import proxqopenai "github.com/psyb0t/docker-proxq/pkg/clients/openai"

client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.example.com",
    APIKey:       "sk-...",
})
```

Same code, same types, same return values. The [client docs](https://psyb0t.github.io/docker-proxq/openai-client/) cover the proxq config it needs and how streaming behaves.

## Agent integrations

The [skill](https://github.com/psyb0t/docker-proxq/tree/main/.agents/skills/proxq) lets an AI agent submit requests to a running proxq and collect the results. Claude Code:

```bash
claude plugin marketplace add psyb0t/agents
claude plugin install proxq@psyb0t
```

Codex and OpenClaw are covered in [agent integrations](https://psyb0t.github.io/docker-proxq/agents/). To change this repository, read [`AGENTS.md`](https://github.com/psyb0t/docker-proxq/blob/main/AGENTS.md).

## Development

Docker, Make and Bash are all you need. Go runs in a container.

```bash
make lint            # go fix -diff and golangci-lint with all the annoying linters enabled
make test            # unit + integration tests (race detector on, Redis via testcontainers)
make test-coverage   # tests with the 90% coverage threshold
make build           # docker build
```

Details in the [development docs](https://psyb0t.github.io/docker-proxq/development/).

## Sponsor

<a href="https://www.buymeacoffee.com/psyb0t"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-blue.png" alt="Buy Me a Coffee" height="60" width="217"></a>

## License

[MIT](https://github.com/psyb0t/docker-proxq/blob/main/LICENSE). Do whatever you want. If it breaks, you get to keep both pieces.
