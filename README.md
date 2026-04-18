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
  - [The important one](#the-important-one)
  - [Proxy settings](#proxy-settings)
  - [Caching](#caching)
  - [HTTP server](#http-server)
- [API](#api)
  - [Send a request](#send-a-request-any-method-any-path)
  - [Poll for result](#poll-for-result)
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
 |<- {result} ---- |                 |               |
 |                 |                 |               |
 |-- PUT /big ---> | --------- direct proxy ------> |
 |<- {response} -- | <----------------------------- |
```

Most requests go through the meat grinder:

1. **Accept** — proxq takes your HTTP request (any method, any path, any body)
2. **Queue** — shoves it into Redis via [asynq](https://github.com/hibiken/asynq) and immediately hands you a job ID
3. **Forward** — a worker picks it up and fires it at the upstream server with all your original headers, body, and proxy headers (`X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`)
4. **Store** — the upstream response (status, headers, body) gets saved as the job result
5. **Poll** — you come back whenever you want and grab the result

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
      PROXQ_UPSTREAM_URL: http://your-api:3000
      PROXQ_REDIS_ADDR: redis:6379
      PROXQ_LISTENADDRESS: 0.0.0.0:8080
    depends_on:
      - redis

  redis:
    image: redis:7-alpine
    restart: unless-stopped
```

That's it. Your API is now async. You're welcome.

## Configuration

Everything's an env var. No YAML, no TOML, no config files to lose in production.

### The important one

| Variable | Description |
|---|---|
| `PROXQ_UPSTREAM_URL` | Where to send the requests. **Required.** If you don't set this, proxq will tell you and die. |

### Proxy settings

| Variable | Default | What it does |
|---|---|---|
| `PROXQ_REDIS_ADDR` | `127.0.0.1:6379` | Redis. You need one. |
| `PROXQ_REDIS_PASSWORD` | | Redis password, if you're into that |
| `PROXQ_REDIS_DB` | `0` | Redis DB number |
| `PROXQ_CONCURRENCY` | `10` | How many workers hammer upstream simultaneously |
| `PROXQ_QUEUE` | `default` | asynq queue name |
| `PROXQ_UPSTREAM_TIMEOUT` | `5m` | How long to wait for upstream before giving up on life |
| `PROXQ_TASK_RETENTION` | `1h` | How long completed jobs stick around in Redis |
| `PROXQ_MAX_REQUEST_BODY_SIZE` | `10485760` | 10MB. Max body size for queued requests. |
| `PROXQ_DIRECT_PROXY_THRESHOLD` | `10485760` | Requests with `Content-Length` above this bypass the queue and get proxied directly to upstream. Chunked transfers always bypass. Set to `0` to disable. |
| `PROXQ_DIRECT_PROXY_PATHS` | | Comma-separated regexes. Requests matching any pattern bypass the queue. Example: `^/uploads,^/ws,^/stream` |
| `PROXQ_JOBS_PATH` | `/__jobs` | Base path for the jobs API. All examples in this doc use the default — yours will differ if you change it. |

### Caching

Because why hit upstream twice when once was painful enough.

| Variable | Default | What it does |
|---|---|---|
| `CACHE_MODE` | `none` | `none` = no cache. `memory` = in-process LRU. `redis` = Redis-backed. |
| `CACHE_TTL` | `5m` | How long cached responses stay fresh |
| `CACHE_MAX_ENTRIES` | `10000` | Max entries for in-memory cache before LRU kicks the oldest out |
| `CACHE_REDIS_ADDR` | `127.0.0.1:6379` | Separate Redis for cache (when `CACHE_MODE=redis`) |
| `CACHE_REDIS_PASSWORD` | | Cache Redis password |
| `CACHE_REDIS_DB` | `0` | Cache Redis DB |

Cache rules:
- **Any method** gets cached. Same POST with the same body? Cache hit. Different body? Cache miss.
- Only **2xx** responses get cached. Your 500s aren't worth remembering.
- Cache key = `sha256(method + url + headers + body)`. Volatile headers like `X-Request-Id` are excluded via `CacheKeyExcludeHeaders`.

### HTTP server

| Variable | Default |
|---|---|
| `PROXQ_LISTENADDRESS` | `127.0.0.1:8080` |

## API

### Send a request (any method, any path)

Anything that doesn't match `PROXQ_JOBS_PATH` (default `/__jobs`) gets intercepted and queued — unless it triggers [direct proxy bypass](#direct-proxy-bypass).

```
POST /api/heavy-computation HTTP/1.1
Content-Type: application/json

{"data": "lots of it"}

→ 202 Accepted
{"jobId": "550e8400-e29b-41d4-a716-446655440000"}
```

### Poll for result

```
GET {PROXQ_JOBS_PATH}/550e8400-e29b-41d4-a716-446655440000

→ 200 OK
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "result": {
    "statusCode": 200,
    "headers": {"Content-Type": ["application/json"]},
    "body": "eyJvayI6dHJ1ZX0="
  },
  "completedAt": "2025-01-01T00:00:00Z"
}
```

`result.body` is base64-encoded because raw bytes in JSON is a war crime.

Statuses: `queued` → `running` → `completed` or `failed`.

### Cancel a job

```
DELETE {PROXQ_JOBS_PATH}/550e8400-e29b-41d4-a716-446655440000

→ 200 {"status": "cancelled"}
→ 404 (already gone or never existed)
```

## Direct proxy bypass

Not everything needs the queue. These requests skip it entirely and get proxied straight to upstream — single transfer, no buffering, synchronous response:

- **Path rules** (`PROXQ_DIRECT_PROXY_PATHS`) — comma-separated regexes matched against the request path. Example: `^/uploads,^/ws,^/stream`
- **Chunked transfers** (`Transfer-Encoding: chunked`) — size unknown, could be huge. Always bypassed.
- **Large bodies** (`Content-Length` > `PROXQ_DIRECT_PROXY_THRESHOLD`) — no point buffering a 1GB ISO into Redis.
- **WebSocket connections** (`Connection: upgrade` + `Upgrade: websocket`) — persistent bidirectional, obviously can't queue these.

The client gets the upstream response directly instead of a job ID. Set `PROXQ_DIRECT_PROXY_THRESHOLD=0` to disable the body size check (chunked and WebSocket bypass still applies).

## Architecture

```
docker-proxq/
├── cmd/                    # the main() nobody reads
├── internal/
│   ├── app/                # wiring: asynq + HTTP server + cache
│   ├── config/             # env vars → struct, no magic
│   ├── proxy/              # the asynq job handlers
│   └── testinfra/          # testcontainers helpers
├── tests/
│   ├── e2e_test.go         # real Docker containers, real Redis, real tests
│   └── .fixtures/
│       └── upstream.js     # Node.js echo server for e2e
├── Dockerfile
└── Makefile
```

Built on:
- **[aichteeteapee](https://github.com/psyb0t/aichteeteapee)** — HTTP forwarding engine (the proxy guts, header stripping, caching integration)
- **[common-go](https://github.com/psyb0t/common-go)** — cache package (in-memory LRU + Redis implementations)
- **[asynq](https://github.com/hibiken/asynq)** — Redis-backed task queue (the job management layer)

## Development

```bash
make dep       # vendor dependencies
make lint      # golangci-lint with all the annoying linters enabled
make test      # unit tests

# e2e tests — spins up Redis + Node.js upstream + proxq
# in Docker via testcontainers. No manual setup needed.
cd tests && go test -v -timeout 10m ./...

make build     # docker build
```

## License

Do whatever you want. If it breaks, you get to keep both pieces.
