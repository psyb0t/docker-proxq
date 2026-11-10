---
title: "Architecture"
description: "proxq internals: one Go process with an HTTP server and an asynq worker on one Redis, the request path, job lifecycle and shutdown."
nav_order: 11
---

# Architecture

proxq is one Go binary. A single process runs the HTTP server that accepts requests and the worker that forwards them, and both talk to one Redis server. Run several proxq processes against the same Redis and queue, and they share the work and the jobs API.

## Components

| Part | Code | Does |
| --- | --- | --- |
| Entry point | [`cmd/main.go`](https://github.com/psyb0t/docker-proxq/blob/main/cmd/main.go) | Resolves the config path, configures logging, calls `app.Run` |
| Config | [`internal/config`](https://github.com/psyb0t/docker-proxq/tree/main/internal/config) | Parses YAML, applies defaults, validates, sorts upstreams |
| Wiring | [`internal/app`](https://github.com/psyb0t/docker-proxq/tree/main/internal/app) | Builds the asynq client, inspector and worker server, the cache, and the HTTP routes |
| Request handler | [`internal/proxy/handler.go`](https://github.com/psyb0t/docker-proxq/blob/main/internal/proxy/handler.go) | Picks the upstream, decides queue or direct proxy, enqueues |
| Worker | [`internal/proxy/worker.go`](https://github.com/psyb0t/docker-proxq/blob/main/internal/proxy/worker.go) | Forwards a queued request, stores the response as the job result |
| Jobs API | [`internal/proxy/jobs_handler.go`](https://github.com/psyb0t/docker-proxq/blob/main/internal/proxy/jobs_handler.go) | Status, content and cancel over the asynq inspector |
| Public types | [`pkg/types`](https://github.com/psyb0t/docker-proxq/tree/main/pkg/types) | Header names shared with clients |
| OpenAI client | [`pkg/clients/openai`](https://github.com/psyb0t/docker-proxq/tree/main/pkg/clients/openai) | An openai-go client that goes through proxq |

Built on:

- [asynq](https://github.com/hibiken/asynq): the Redis task queue, retries, retention and task inspection;
- [aichteeteapee](https://github.com/psyb0t/aichteeteapee): the HTTP server and router, and the forwarding code (`prawxxey`) that strips hop-by-hop headers, sets `X-Forwarded-*` and talks to the cache;
- [common-go](https://github.com/psyb0t/common-go): the memory and Redis cache implementations.

## Request path

1. The router runs three middlewares on every request: request ID, request logging and panic recovery.
2. `GET {jobsPath}/{id}/content`, `GET {jobsPath}/{id}` and `DELETE {jobsPath}/{id}` go to the jobs API. Every other method and path goes to the request handler.
3. The handler finds the upstream by prefix. No match: `502`.
4. A WebSocket upgrade is reverse-proxied. A chunked body, a body over `directProxyThreshold`, or a path filter hit is direct-proxied or redirected. See [routing](routing.md).
5. Otherwise the handler reads the body up to `maxBodySize`, builds a task with the method, stripped URL, headers, body, client IP and scheme, the retry delay and the cache exclude list, and enqueues it with a new UUID as the task ID. The task carries `MaxRetry`, `Retention` and, when set, `Timeout`.
6. The handler answers `202` with the job ID.

## Job lifecycle

1. A worker takes the task from the configured queue. `concurrency` workers run at once.
2. With caching on, the worker looks the request up in the cache first.
3. Otherwise it sends the request to the upstream and reads the response, up to 100 MiB.
4. The status code, headers and body become the task result in Redis. A `2xx` response is also cached.
5. A transport error fails the attempt. asynq retries it after the [retry delay](configuration.md#retries) until `maxRetries` is used up, then archives the task, which the jobs API reports as `failed`.
6. Completed and archived tasks are deleted after `taskRetention`.

asynq task states map to the API status like this: pending, scheduled and aggregating are `queued`; active and retry are `running`; completed is `completed`; archived is `failed`.

## Shutdown

`SIGINT` or `SIGTERM` stops the HTTP server and shuts the asynq worker server down. asynq gives running tasks time to finish and puts unfinished ones back in the queue, so another proxq process, or this one after a restart, picks them up.
