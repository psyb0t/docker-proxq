# proxq

[![Docker Hub](https://img.shields.io/docker/pulls/psyb0t/proxq?style=flat-square)](https://hub.docker.com/r/psyb0t/proxq)
[![Go Reference](https://pkg.go.dev/badge/github.com/psyb0t/proxq.svg)](https://pkg.go.dev/github.com/psyb0t/proxq)

The honey badger of HTTP proxies. Takes your request, throws it in a Redis-backed job queue, and deals with it when it damn well pleases. You get a job ID back instantly — come back later to pick up the goods.

Think of it as "I'll get back to you" as a service. Every HTTP request becomes an async job. No more hanging connections, no more timeouts, no more "please hold." Fire and forget, poll when ready.

Oh, and it caches too. Because hitting the same endpoint twice is for people who enjoy watching paint dry.

## Table of Contents

- [What it actually does](#what-it-actually-does)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [API](#api)
  - [Send a request](#send-a-request-any-method-any-path)
  - [Check job status](#check-job-status)
  - [Get the response](#get-the-response)
  - [Cancel a job](#cancel-a-job)
- [Direct proxy bypass](#direct-proxy-bypass)
- [Architecture](#architecture)
- [Development](#development)
- [License](#license)

## What it actually does

```
You              proxq            Redis          Your API
 |                 |                 |               |
 |-- POST /foo --> |                 |               |
 |<- 202 {jobId} - |                 |               |
 |                 |-- enqueue ----> |               |
 |  (go touch      |                 |               |
 |   grass)        |                 | <- worker --- |
 |                 |                 |    wakes up   |
 |                 |                 | -----------> (upstream call)
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

1. **Accept** — proxq takes your HTTP request (any method, any path, any body)
2. **Route** — matches the request path to an upstream via longest-prefix match
3. **Queue** — shoves it into Redis via [asynq](https://github.com/hibiken/asynq) and immediately hands you a job ID
4. **Forward** — a worker picks it up and fires it at the upstream server with all your original headers, body, and proxy headers (`X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`)
5. **Store** — the upstream response (status, headers, body) gets saved as the job result
6. **Poll** — you come back whenever you want and grab the result

Hop-by-hop headers get stripped per RFC 2616 because we're civilized like that.

**Large uploads and chunked transfers** bypass the queue entirely and get proxied straight to upstream — no buffering, no double transfer, no memory bomb. WebSocket connections also get proxied directly.

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

```yaml
listenAddress: "0.0.0.0:8080"      # HTTP listen address

redis:
  addr: "127.0.0.1:6379"           # Redis address
  password: ""                      # Redis password
  db: 0                             # Redis DB number

queue: "default"                    # asynq queue name
concurrency: 10                     # how many workers hammer upstream simultaneously
jobsPath: "/__jobs"                 # base path for the jobs API
taskRetention: "1h"                 # how long completed jobs stick around in Redis
```

### Upstreams

Multiple upstreams with path-prefix routing. Longest prefix wins. The prefix is stripped before forwarding.

```yaml
upstreams:
  - prefix: "/api"
    url: "http://api-server:3000"
    timeout: "5m"                   # per-upstream request timeout
    maxRetries: 0                     # asynq retry count (0 = no retries)
    retryDelay: "30s"               # fixed delay between retries (0 = exponential backoff)
    maxBodySize: 10485760           # max queued body size (10MB)
    directProxyThreshold: 10485760  # body size bypass threshold (0 = disable)
    directProxyMode: "proxy"        # proxy or redirect (307)
    pathFilter:
      mode: "blacklist"             # blacklist or whitelist
      patterns:
        - "^/api/uploads"           # these paths bypass the queue

  - prefix: "/web"
    url: "http://web-server:8080"
```

`/api/users?page=1` → `http://api-server:3000/users?page=1` (prefix stripped, query preserved).

Rules: with a single upstream, `prefix: "/"` is allowed (catch-all). With multiple upstreams, each must have its own distinct prefix — no root `"/"` and no overlapping prefixes. Requests that don't match any prefix get `502 Bad Gateway`.

### Caching

```yaml
cache:
  mode: "none"          # none, memory, redis
  ttl: "5m"             # how long cached responses stay fresh
  maxEntries: 10000     # max entries for in-memory LRU
  redisKeyPrefix: "proxq:"
```

When `mode: redis`, cache uses the same Redis instance as the job queue. Keys are namespaced under the prefix so they don't collide with job data.

Cache rules:
- **Any method** gets cached. Same POST with the same body? Cache hit. Different body? Cache miss.
- Only **2xx** responses get cached. Your 500s aren't worth remembering.
- Cache key = `sha256(method + url + headers + body)`. Volatile headers (`X-Request-ID`, `X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`) are excluded from the key so they don't bust the cache.
- Cached responses include an `X-Cache-Status` header: `HIT` for cache hits, `MISS` for fresh upstream responses.

## API

### Send a request (any method, any path)

Anything that doesn't match `jobsPath` gets intercepted and routed to an upstream — unless it triggers [direct proxy bypass](#direct-proxy-bypass).

```
POST /api/heavy-computation HTTP/1.1
Content-Type: application/json

{"data": "lots of it"}

→ 202 Accepted
{"jobId": "550e8400-e29b-41d4-a716-446655440000"}
```

### Check job status

```
GET /__jobs/550e8400-e29b-41d4-a716-446655440000

→ 200 OK
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "completedAt": "2025-01-01T00:00:00Z"
}
```

Statuses: `queued` → `running` → `completed` or `failed`.

A job is **completed** when the HTTP round-trip finishes — even if upstream returned 404 or 500. A job is **failed** only when the transport itself broke (network error, timeout, etc.).

### Get the response

```
GET /__jobs/550e8400-e29b-41d4-a716-446655440000/content

→ 200 OK
Content-Type: application/json

{"ok": true}
```

Replays the upstream response exactly — status code, headers, body. As if you'd called upstream directly.

Returns `404` if the job isn't done yet or doesn't exist. When proxq itself returns 404 (not the upstream), the response includes an `X-Proxq-Source: proxq` header so you can tell the difference.

### Cancel a job

```
DELETE /__jobs/550e8400-e29b-41d4-a716-446655440000

→ 200 {"status": "cancelled"}
→ 404 (already gone or never existed)
```

## Direct proxy bypass

Not everything needs the queue. These requests skip it entirely:

- **Path filter** (per-upstream `pathFilter`) — in `blacklist` mode (default), matching paths bypass the queue. In `whitelist` mode, only matching paths get queued, everything else bypasses.
- **Chunked transfers** (`Transfer-Encoding: chunked`) — size unknown, could be huge. Always bypassed.
- **Large bodies** (`Content-Length` > upstream's `directProxyThreshold`) — no point buffering a 1GB ISO into Redis.
- **WebSocket connections** (`Connection: upgrade` + `Upgrade: websocket`) — persistent bidirectional, obviously can't queue these.

Bypassed requests are either reverse-proxied through proxq (`directProxyMode: proxy`, default) or 307-redirected to upstream (`redirect` mode, requires upstream to be reachable by the client).

## Architecture

```
docker-proxq/
├── cmd/                        # the main() nobody reads
├── internal/
│   ├── app/                    # wiring: asynq + HTTP server + cache
│   ├── config/                 # YAML config parsing
│   ├── proxy/                  # handler, worker, job types
│   └── testinfra/              # testcontainers helpers
├── tests/                      # e2e tests (Docker-based)
├── config.yaml.example         # full config reference
├── Dockerfile
└── Makefile
```

Built on:
- **[aichteeteapee](https://github.com/psyb0t/aichteeteapee)** — HTTP forwarding engine (the proxy guts, header stripping, caching integration)
- **[common-go](https://github.com/psyb0t/common-go)** — cache package (in-memory LRU + Redis implementations)
- **[asynq](https://github.com/hibiken/asynq)** — Redis-backed task queue (the job management layer)

## Use cases

### Slow APIs behind reverse proxies with short timeouts

Your CDN or reverse proxy (nginx, Cloudflare, etc.) has a request timeout. Your backend sometimes takes longer. Stick proxq between them:

```yaml
upstreams:
  - prefix: "/"
    url: "http://slow-backend:8080"
    timeout: "10m"
```

Client sends request → gets a job ID back instantly (reverse proxy is happy, fast response) → worker takes as long as the backend needs → client polls for the result whenever.

### Webhook relay

Fire webhooks without blocking the sender. Queue them, deliver at your own pace, retry if needed:

```yaml
upstreams:
  - prefix: "/hooks"
    url: "http://webhook-processor:8080"
    timeout: "30s"
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

  - prefix: "/ml"
    url: "http://ml-service:8080"
    timeout: "15m"

  - prefix: "/uploads"
    url: "http://file-server:9000/storage"
    timeout: "10m"
    maxBodySize: 1073741824
    directProxyMode: "redirect"
```

## Development

```bash
make dep            # vendor dependencies
make lint           # golangci-lint with all the annoying linters enabled
make test           # unit + integration tests
make test-coverage  # tests with 90% coverage check

# e2e tests — spins up Redis + Node.js upstream + proxq
# in Docker via testcontainers. No manual setup needed.
cd tests && go test -v -timeout 10m ./...

make build          # docker build
```

## License

Do whatever you want. If it breaks, you get to keep both pieces.
