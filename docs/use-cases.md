---
title: "Use cases"
description: "Example proxq configs: slow APIs behind short proxy timeouts, webhook relays, mixed sync and async APIs, media processing, multi-service gateways."
nav_order: 7
---

# Use cases

Each example is the `upstreams` part of a config. The other fields are on the [configuration](configuration.md) page.

## Slow APIs behind short proxy timeouts

Your CDN or reverse proxy (nginx, Cloudflare and the like) cuts requests off after a fixed time, and your backend sometimes takes longer. Put proxq between them:

```yaml
upstreams:
  - prefix: "/"
    url: "http://slow-backend:8080"
    timeout: "10m"
    maxRetries: 2
```

The client gets a job ID back at once, so the reverse proxy is happy. The worker gives the backend up to ten minutes, and the client polls for the result whenever it likes.

## Webhook relay

Accept webhooks without making the sender wait, deliver them at your own pace, and retry when the receiver is down:

```yaml
upstreams:
  - prefix: "/hooks"
    url: "http://webhook-processor:8080"
    timeout: "30s"
    maxRetries: 5
    retryDelay: "10s"
```

Retries cover transport failures only. A receiver that answers `500` completes the job; see [retries](configuration.md#retries).

## Mixed sync and async API

Some endpoints are fast (auth, health), others are slow (reports, exports). Queue the slow ones and let the fast ones through:

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

Auth and health requests skip the queue and hit the backend directly. Everything else is queued.

## Image and video processing

Accept large uploads, queue them for processing, and let the client check back later:

```yaml
upstreams:
  - prefix: "/process"
    url: "http://media-worker:9000"
    timeout: "30m"
    maxBodySize: 1073741824
    directProxyThreshold: -1
```

`directProxyThreshold: -1` turns off the size check, so bodies of any size are queued up to `maxBodySize` (1 GiB here). The whole body is stored in Redis, so size Redis memory for it. Chunked uploads still go direct.

## Multi-service gateway

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

Uploads over 10 MiB to `/uploads` get a `307` to the file server, which the client must be able to reach. Smaller ones are queued.

## Slow LLM calls from Go

The [OpenAI Go client](openai-client.md) sends openai-go SDK calls through proxq without changing the calling code.
