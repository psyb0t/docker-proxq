# proxq

[![Docker Hub](https://img.shields.io/docker/pulls/psyb0t/proxq?style=flat-square)](https://hub.docker.com/r/psyb0t/proxq)
[![Go Reference](https://pkg.go.dev/badge/github.com/psyb0t/proxq.svg)](https://pkg.go.dev/github.com/psyb0t/proxq)

The honey badger of HTTP proxies. Takes your request, throws it in a Redis-backed job queue, and deals with it when it damn well pleases. You get a job ID back instantly — come back later to pick up the goods.

Think of it as "I'll get back to you" as a service. Every HTTP request becomes an async job. No more hanging connections, no more timeouts, no more "please hold." Fire and forget, poll when ready.

Oh, and it caches too. Because hitting the same endpoint twice is for people who enjoy watching paint dry.

## Table of Contents

- [How it works](#how-it-works)
- [Quick start](#quick-start)
- [Configuration](#configuration)
  - [Global settings](#global-settings)
  - [Upstreams](#upstreams)
  - [Path filter](#path-filter)
  - [Retries](#retries)
  - [Caching](#caching)
- [API](#api)
  - [Submit a request](#submit-a-request)
  - [Get job status](#get-job-status)
  - [Get job content](#get-job-content)
  - [Cancel a job](#cancel-a-job)
- [Direct proxy bypass](#direct-proxy-bypass)
- [Routing](#routing)
  - [Prefix matching](#prefix-matching)
  - [Prefix stripping](#prefix-stripping)
  - [Upstream URL with path](#upstream-url-with-path)
  - [Validation rules](#validation-rules)
- [Headers](#headers)
- [Client libraries](#client-libraries)
- [Use cases](#use-cases)
- [Architecture](#architecture)
- [Development](#development)
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

Most requests go through the meat grinder:

1. **Accept** — proxq takes your HTTP request (any method, any path, any body).
2. **Route** — matches the request path to an upstream via longest-prefix match.
3. **Queue** — serializes the whole thing (method, URL, headers, body) into Redis via [asynq](https://github.com/hibiken/asynq) and hands you a job ID immediately.
4. **Forward** — a worker picks it up and fires it at upstream with all your original headers, body, and proxy headers (`X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`). Hop-by-hop headers get stripped per RFC 7230 because we're civilized like that.
5. **Store** — the upstream response (status code, headers, body) gets saved as the job result.
6. **Poll** — you come back whenever you want and grab the result.

**Large uploads, chunked transfers, and WebSocket connections** bypass the queue entirely and get proxied straight to upstream — no buffering, no double transfer, no memory bomb.

## Quick start

```yaml
services:
  proxq:
    image: psyb0t/proxq
    ports:
      - "8080:8080"
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

That's it. Your API is now async. You're welcome.

## Configuration

Everything lives in a YAML config file. See [`config.yaml.example`](config.yaml.example) for the full reference.

Config file path is resolved in order: `--config` flag → `PROXQ_CONFIG` env var → `config.yaml` in the current directory.

### Global settings

| Field | Type | Default | Description |
|---|---|---|---|
| `listenAddress` | string | `127.0.0.1:8080` | HTTP server bind address |
| `redis.addr` | string | `127.0.0.1:6379` | Redis server address |
| `redis.password` | string | `""` | Redis password |
| `redis.db` | int | `0` | Redis database number |
| `queue` | string | `default` | asynq queue name |
| `concurrency` | int | `10` | How many workers hammer upstream simultaneously |
| `jobsPath` | string | `/__jobs` | Base path for the jobs API endpoints |
| `taskRetention` | duration | `1h` | How long completed/failed jobs stick around in Redis |

Duration values use Go syntax: `30s`, `5m`, `1h`, `1h30m`.

### Upstreams

Multiple upstreams with path-prefix routing. Longest prefix wins. The prefix is stripped before forwarding.

| Field | Type | Default | Description |
|---|---|---|---|
| `prefix` | string | **required** | URL path prefix for routing. Stripped before forwarding. |
| `url` | string | **required** | Upstream server URL. Can include a path (e.g., `http://api:3000/v2`). |
| `timeout` | duration | `5m` | Per-upstream request timeout |
| `maxRetries` | int | `0` | Retry attempts on failure. `0` = no retries. |
| `retryDelay` | duration | `0` | Fixed delay between retries. `0` = exponential backoff. |
| `maxBodySize` | int | `10485760` | Max request body to queue (bytes). 10 MB default. |
| `directProxyThreshold` | int | `10485760` | Body size above which requests bypass the queue. `0` disables. |
| `directProxyMode` | string | `proxy` | How bypassed requests reach upstream: `proxy` or `redirect` (307). |
| `pathFilter` | object | | See [Path filter](#path-filter). |

```yaml
upstreams:
  - prefix: "/api"
    url: "http://api-server:3000"
    timeout: "5m"
    maxRetries: 3
    retryDelay: "10s"
    pathFilter:
      mode: "blacklist"
      patterns:
        - "^/api/health"

  - prefix: "/files"
    url: "http://file-server:9000/storage"
    timeout: "10m"
    maxBodySize: 1073741824
    directProxyThreshold: 0
```

### Path filter

Per-upstream regex-based filtering. Controls which requests get queued vs direct-proxied.

| Field | Type | Default | Description |
|---|---|---|---|
| `pathFilter.mode` | string | `blacklist` | `blacklist`: matching paths bypass the queue. `whitelist`: only matching paths get queued. |
| `pathFilter.patterns` | list | `[]` | Regex patterns matched against the request path. |

**Blacklist** (default) — matching paths skip the queue:

```yaml
pathFilter:
  mode: "blacklist"
  patterns:
    - "^/api/auth"
    - "^/api/health"
```

Auth and health go straight to upstream. Everything else gets queued.

**Whitelist** — only matching paths get queued:

```yaml
pathFilter:
  mode: "whitelist"
  patterns:
    - "^/api/reports"
    - "^/api/exports"
```

Reports and exports get queued. Everything else goes straight through.

### Retries

Failed jobs can be retried automatically. "Failed" means the transport itself broke — network error, timeout, connection refused. An upstream returning 500 is still a completed job (the 500 response gets stored as the result, because that's what upstream said).

| Field | Type | Default | Description |
|---|---|---|---|
| `maxRetries` | int | `0` | Retry attempts. `0` = no retries. |
| `retryDelay` | duration | `0` | Fixed delay. `0` = exponential backoff (n^4 seconds). |

Exponential backoff schedule:

| Attempt | Delay |
|---|---|
| 1st | 1 second |
| 2nd | 16 seconds |
| 3rd | 81 seconds |
| 4th | ~4 minutes |
| 5th | ~10 minutes |

Or just set a fixed delay:

```yaml
upstreams:
  - prefix: "/"
    url: "http://flaky-backend:8080"
    maxRetries: 5
    retryDelay: "30s"
```

### Caching

| Field | Type | Default | Description |
|---|---|---|---|
| `cache.mode` | string | `none` | `none`, `memory` (in-memory LRU), or `redis` (shared). |
| `cache.ttl` | duration | `5m` | How long cached responses stay fresh |
| `cache.maxEntries` | int | `10000` | Max entries for in-memory LRU |
| `cache.redisKeyPrefix` | string | `proxq:` | Key prefix for Redis cache, so it doesn't collide with job data |

```yaml
cache:
  mode: "redis"
  ttl: "10m"
  redisKeyPrefix: "proxq:"
```

When `mode: redis`, cache uses the same Redis instance as the job queue.

Cache rules:
- **Any method** gets cached. Same POST with the same body? Cache hit. Different body? Cache miss.
- Only **2xx** responses get cached. Your 500s aren't worth remembering.
- Cache key = `sha256(method + url + headers + body)`. Volatile headers (`X-Request-ID`, `X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`) are excluded from the key so they don't bust the cache.
- Cached responses include `X-Cache-Status: HIT`. Fresh upstream responses include `X-Cache-Status: MISS`.

## API

All job endpoints live under `jobsPath` (default `/__jobs`). Every response that proxq generates (not proxied from upstream) carries the `X-Proxq-Source: proxq` header — that's how you tell proxq responses from upstream responses.

### Submit a request

Any request that doesn't hit a job endpoint gets intercepted, routed to an upstream, and queued — unless it triggers a [direct proxy bypass](#direct-proxy-bypass).

```http
POST /api/heavy-computation HTTP/1.1
Content-Type: application/json
Authorization: Bearer token

{"data": "lots of it"}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
X-Proxq-Source: proxq

{"jobId": "550e8400-e29b-41d4-a716-446655440000"}
```

If no upstream matches the request path: `502 Bad Gateway` with `X-Proxq-Source: proxq`.

### Get job status

```http
GET /__jobs/550e8400-e29b-41d4-a716-446655440000 HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "completedAt": "2025-01-01T00:00:00Z"
}
```

Failed job:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "failed",
  "error": "forward request: dial tcp: connection refused"
}
```

Not found: `404 Not Found` with `X-Proxq-Source: proxq`.

**Status lifecycle:**

| Status | What it means | Underlying asynq states |
|---|---|---|
| `queued` | Waiting to be picked up | pending, scheduled, aggregating |
| `running` | Worker is on it, or waiting for a retry | active, retry |
| `completed` | Done. Response stored. Even if upstream returned 4xx/5xx. | completed |
| `failed` | Transport broke after all retries exhausted. | archived |

### Get job content

This is the payoff. Replays the upstream response exactly — status code, headers, body. As if you'd called upstream directly.

```http
GET /__jobs/550e8400-e29b-41d4-a716-446655440000/content HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Custom-Header: from-upstream

{"result": "done"}
```

If the upstream returned 404, you get 404 back — but without `X-Proxq-Source` (because it came from upstream, not proxq).

If the job isn't done yet or doesn't exist: `404 Not Found` with `X-Proxq-Source: proxq`.

**How to tell the difference:**
- `X-Proxq-Source: proxq` present → proxq says "job not ready or doesn't exist"
- `X-Proxq-Source` absent → that's the actual upstream response

### Cancel a job

```http
DELETE /__jobs/550e8400-e29b-41d4-a716-446655440000 HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{"status": "cancelled"}
```

Not found: `404 Not Found` with `X-Proxq-Source: proxq`.

## Direct proxy bypass

Not everything needs the queue. These requests skip it entirely:

| Condition | Why | Checked |
|---|---|---|
| **WebSocket** (`Connection: upgrade` + `Upgrade: websocket`) | Persistent bidirectional. Can't queue that. | First |
| **Path filter match** (per-upstream `pathFilter`) | You said so. | Second |
| **Chunked transfer** (`Transfer-Encoding: chunked`) | Size unknown, could be huge. | Third |
| **Large body** (`Content-Length` > `directProxyThreshold`) | No point buffering a 1 GB upload into Redis. | Fourth |

How bypassed requests reach upstream:

- **`directProxyMode: proxy`** (default) — reverse-proxied through proxq. Client never sees the upstream URL. Streams in both directions.
- **`directProxyMode: redirect`** — proxq responds with `307 Temporary Redirect` to the upstream URL. Client must be able to reach upstream directly.

## Routing

### Prefix matching

Upstreams are sorted by prefix length (longest first). First match wins.

A prefix matches when the request path equals the prefix exactly, or starts with the prefix followed by `/`. This prevents `/api` from accidentally matching `/api2`.

| Request path | `/api` | `/api/v2` | `/` |
|---|---|---|---|
| `/api/users` | match | no | match |
| `/api/v2/users` | match | **match (wins)** | match |
| `/api2/data` | no | no | match |
| `/other` | no | no | match |

### Prefix stripping

The matched prefix is stripped from the request path before forwarding. Query strings are preserved.

| Request | Prefix | Forwarded path |
|---|---|---|
| `GET /api/users?page=1` | `/api` | `GET /users?page=1` |
| `GET /api` | `/api` | `GET /` |
| `POST /uploads/img.png` | `/uploads` | `POST /img.png` |
| `GET /anything` | `/` | `GET /anything` |

### Upstream URL with path

The upstream URL can include a path. The stripped request path gets appended to it.

```yaml
upstreams:
  - prefix: "/files"
    url: "http://storage:9000/bucket/data"
```

| Request | Upstream receives |
|---|---|
| `GET /files/img.png` | `GET http://storage:9000/bucket/data/img.png` |
| `GET /files` | `GET http://storage:9000/bucket/data/` |

### Validation rules

proxq validates your config at startup and refuses to run if something's wrong:

- At least one upstream is required.
- Each upstream needs both `prefix` and `url`.
- Single upstream: `prefix: "/"` is fine (catch-all).
- Multiple upstreams: `prefix: "/"` is **not allowed** — too ambiguous.
- No nested prefixes: `/api` and `/api/v2` together is an error.
- No prefix can conflict with `jobsPath`: `/__jobs` as a prefix when `jobsPath` is `/__jobs` is an error.
- Path filter patterns must be valid regexes.

## Headers

### Set by proxq

| Header | Value | When |
|---|---|---|
| `X-Proxq-Source` | `proxq` | Every response proxq generates: `202` accepted, `502` no match, `500` errors, `307` redirects, `404` from job endpoints, reverse proxy errors. **Never** on responses proxied from upstream. |
| `X-Cache-Status` | `HIT` / `MISS` | On cached responses when caching is enabled. |

### Forwarded to upstream

| Header | Description |
|---|---|
| `X-Forwarded-For` | Original client IP |
| `X-Real-IP` | Original client IP (alternate) |
| `X-Forwarded-Proto` | Original request scheme (`http` or `https`) |

All original request headers are preserved. Hop-by-hop headers (`Connection`, `Keep-Alive`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailers`, `Transfer-Encoding`, `Upgrade`) are stripped per RFC 7230.

## Client libraries

### OpenAI Go client

Drop-in replacement for [`openai-go`](https://github.com/openai/openai-go). Swap one line and all your SDK calls go through proxq transparently — chat completions, embeddings, images, audio, everything.

```go
import proxqopenai "github.com/psyb0t/proxq/pkg/clients/openai"

// Before: client := openai.NewClient(option.WithAPIKey("sk-..."))
// After:
client := proxqopenai.NewClient(proxqopenai.Config{
    ProxqBaseURL: "https://proxq.example.com",
    APIKey:       "sk-...",
})

// Same code, same types, same return values
resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
    Model:    openai.ChatModelGPT4o,
    Messages: []openai.ChatCompletionMessageParamUnion{
        openai.UserMessage("hello"),
    },
})

fmt.Println(resp.Choices[0].Message.Content)
```

The client injects a custom `http.RoundTripper` into the SDK. Non-streaming requests get enqueued, polled, and returned as if you called OpenAI directly. Streaming and direct-proxied responses pass through as-is. Your `HTTPClient` settings (TLS config, timeouts, cookie jar) are fully preserved.

See [`pkg/clients/openai/README.md`](pkg/clients/openai/README.md) for the full docs.

## Use cases

### Slow APIs behind reverse proxies with short timeouts

Your CDN or reverse proxy (nginx, Cloudflare, etc.) has a request timeout. Your backend sometimes takes longer. Stick proxq between them:

```yaml
upstreams:
  - prefix: "/"
    url: "http://slow-backend:8080"
    timeout: "10m"
    maxRetries: 2
```

Client sends request → gets a job ID back instantly (reverse proxy is happy, fast response) → worker takes as long as the backend needs → client polls for the result whenever.

### Webhook relay

Fire webhooks without blocking the sender. Queue them, deliver at your own pace, retry if needed:

```yaml
upstreams:
  - prefix: "/hooks"
    url: "http://webhook-processor:8080"
    timeout: "30s"
    maxRetries: 5
    retryDelay: "10s"
```

### Mixed sync/async API

Some endpoints are fast (auth, health), others are slow (reports, exports). Queue the slow ones, let the fast ones pass through:

```yaml
upstreams:
  - prefix: "/api"
    url: "http://backend:3000"
    timeout: "5m"
    pathFilter:
      mode: "blacklist"
      patterns:
        - "^/api/auth"
        - "^/api/health"
```

Auth and health requests bypass the queue and hit the backend directly. Everything else gets queued.

### Image/video processing pipeline

Accept large uploads, queue them for processing, let the client check back later:

```yaml
upstreams:
  - prefix: "/process"
    url: "http://media-worker:9000"
    timeout: "30m"
    maxBodySize: 1073741824
    directProxyThreshold: 0
```

`directProxyThreshold: 0` disables body-size bypass — everything gets queued regardless of size (up to `maxBodySize`).

### Multi-service gateway

Route to different backends by path, each with its own rules:

```yaml
upstreams:
  - prefix: "/api"
    url: "http://api:3000"
    timeout: "5m"
    maxRetries: 3

  - prefix: "/ml"
    url: "http://ml-service:8080"
    timeout: "15m"

  - prefix: "/uploads"
    url: "http://file-server:9000/storage"
    timeout: "10m"
    maxBodySize: 1073741824
    directProxyMode: "redirect"
```

## Architecture

```
docker-proxq/
├── cmd/                        # the main() nobody reads
├── internal/
│   ├── app/                    # wiring: asynq + HTTP server + cache
│   ├── config/                 # YAML config parsing, validation, defaults
│   ├── proxy/                  # handler, worker, job types, jobs API
│   └── testinfra/              # testcontainers helpers
├── pkg/
│   ├── types/                  # public constants (headers)
│   └── clients/openai/         # drop-in OpenAI SDK client
├── tests/                      # e2e tests (Docker-based)
├── config.yaml.example         # full config reference
├── Dockerfile
└── Makefile
```

Built on:
- **[asynq](https://github.com/hibiken/asynq)** — Redis-backed task queue (the job management layer)
- **[aichteeteapee](https://github.com/psyb0t/aichteeteapee)** — HTTP forwarding engine (the proxy guts, header stripping, caching)
- **[common-go](https://github.com/psyb0t/common-go)** — cache package (in-memory LRU + Redis implementations)

## Development

```bash
make dep            # vendor dependencies
make lint           # golangci-lint with all the annoying linters enabled
make test           # unit + integration tests (race detector on)
make test-coverage  # tests with 90% coverage threshold

# e2e tests — spins up Redis + upstream + proxq
# via testcontainers. No manual setup needed.
cd tests && go test -v -timeout 10m ./...

make build          # docker build
```

## License

Do whatever you want. If it breaks, you get to keep both pieces.
