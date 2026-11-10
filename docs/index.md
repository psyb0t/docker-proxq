---
title: "Home"
description: "proxq is an async HTTP proxy: every request becomes a Redis-backed job, you get a job ID at once and fetch the upstream response later."
nav_order: 1
permalink: /
seo:
  type: WebSite
---

# proxq

The honey badger of HTTP proxies. proxq takes your request, throws it in a Redis-backed job queue, and answers right away with a job ID. A worker forwards the request to your upstream in its own time, stores the response, and you come back for it whenever you like.

Think of it as "I'll get back to you" as a service. No hanging connections, no gateway timeouts while a slow backend thinks. It caches too, if you ask it to.

## How it works

```
Client           proxq            Redis          Upstream
 |                 |                 |               |
 |-- POST /foo --> |                 |               |
 |<- 202 {jobId} - |                 |               |
 |                 |-- enqueue ----> |               |
 |                 |                 | <- worker --- |
 |                 |                 | -----------> (request)
 |                 |                 | <----------- (response)
 |-- GET /{jobId}->|                 |               |
 |<- {status} ---- |                 |               |
 |-- GET /content->|                 |               |
 |<- {response} -- |                 |               |
 |                 |                 |               |
 |-- PUT /big ---> | --------- direct proxy ------>  |
 |<- {response} -- | <-----------------------------  |
```

1. **Accept.** proxq takes any HTTP request: any method, any path, any body.
2. **Route.** The longest matching path prefix picks the upstream, and the prefix is stripped.
3. **Queue.** Method, URL, headers and body go into Redis through [asynq](https://github.com/hibiken/asynq), and the client gets `202 Accepted` with a job ID.
4. **Forward.** A worker sends the request to the upstream with the original headers plus `X-Forwarded-For`, `X-Real-IP` and `X-Forwarded-Proto`. Hop-by-hop headers are dropped.
5. **Store.** The upstream status, headers and body become the job result.
6. **Poll.** The client reads the job status, then the stored response.

WebSocket upgrades, chunked uploads, bodies over a size threshold, and paths you pick with a filter skip the queue and are proxied straight to the upstream. The [routing](routing.md) page has the rules.

## Quick start

proxq needs a Redis server and one YAML config file. This Compose file runs both, with one upstream catching every path:

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

Replace `http://your-api:3000` with your backend, then submit a request and collect the answer:

```bash
docker compose up -d

JOB_ID=$(
  curl -s -X POST localhost:8080/reports \
    -H 'Content-Type: application/json' \
    -d '{"month": "2026-01"}' |
    jq -r .jobId
)

curl -s "localhost:8080/__jobs/$JOB_ID"
curl -si "localhost:8080/__jobs/$JOB_ID/content"
```

The first call returns `{"jobId": "..."}` at once. The status call reports `queued`, `running`, `completed` or `failed`. Once the job is `completed`, `/content` replays the upstream response with its original status code, headers and body.

proxq has no authentication of its own, which is why the port above is bound to loopback. Read [security](security.md) before you expose it.

## Where to go next

| Page | What it covers |
| --- | --- |
| [Configuration](configuration.md) | Every config field with its default, the config path, logging variables |
| [Jobs API](api.md) | Submitting, status, content, cancel, headers, retries |
| [Routing and bypass](routing.md) | Prefix matching and stripping, direct proxy rules, redirect mode |
| [Caching](caching.md) | Memory and Redis caches, the cache key, what gets cached |
| [OpenAI Go client](openai-client.md) | The openai-go wrapper that sends SDK calls through proxq |
| [Use cases](use-cases.md) | Example configs for common setups |
| [Agent integrations](agents.md) | The skill for Claude Code, Codex and OpenClaw |
| [Security](security.md) | What proxq exposes and what to put in front of it |
| [Troubleshooting](troubleshooting.md) | Startup errors and common runtime problems |
| [Architecture](architecture.md) | Components, data flow and job lifecycle |
| [Development](development.md) | Building, linting, testing and CI |
