---
title: "Routing and bypass"
description: "How proxq picks an upstream by path prefix, strips the prefix, and which requests skip the queue for a direct proxy or a 307 redirect."
nav_order: 4
---

# Routing and bypass

Every request outside the [jobs API](api.md) goes through the same steps: pick the upstream, decide whether it is queued or sent direct, then forward it with the prefix removed.

## Prefix matching

A prefix matches when the request path equals it, or starts with it followed by `/`. That keeps `/api` from matching `/api2`. The prefix `/` matches every path.

| Request path | `/api` | `/files` | `/` (only upstream) |
| --- | --- | --- | --- |
| `/api/users` | match | no | match |
| `/api` | match | no | match |
| `/api2/data` | no | no | match |
| `/other` | no | no | match |

Startup [validation](configuration.md#validation) refuses nested prefixes and refuses `/` when there is more than one upstream, so at most one upstream can match a path. A path that matches none gets `502 Bad Gateway` with `X-Proxq-Source: proxq`.

## Prefix stripping

The matched prefix is removed before forwarding, and the query string is kept. When nothing is left, the path becomes `/`.

| Request | Prefix | Forwarded as |
| --- | --- | --- |
| `GET /api/users?page=1` | `/api` | `GET /users?page=1` |
| `GET /api` | `/api` | `GET /` |
| `POST /uploads/img.png` | `/uploads` | `POST /img.png` |
| `GET /anything` | `/` | `GET /anything` |

## Upstream URL with a path

The upstream `url` can carry a path. The stripped request path is appended to it, and a trailing `/` on the `url` is ignored.

```yaml
upstreams:
  - prefix: "/files"
    url: "http://storage:9000/bucket/data"
```

| Request | Upstream receives |
| --- | --- |
| `GET /files/img.png` | `GET http://storage:9000/bucket/data/img.png` |
| `GET /files` | `GET http://storage:9000/bucket/data/` |

## Direct proxy bypass

Some requests skip the queue and go straight to the upstream. proxq checks, in this order:

| Condition | Why |
| --- | --- |
| WebSocket upgrade: `Connection: upgrade` and `Upgrade: websocket` | A two-way connection cannot be queued |
| `Transfer-Encoding: chunked` | The size is unknown and could be huge |
| `Content-Length` over `directProxyThreshold` | No point buffering a 1 GB upload into Redis |
| The upstream's `pathFilter` says so | You asked for it |

The threshold check is off when `directProxyThreshold` is negative. Set to `0` or left out, it is 10 MiB; see [upstream settings](configuration.md#upstream-settings).

### Path filter

`pathFilter.patterns` are Go regular expressions matched against the request path as the client sent it, before the prefix is stripped. An upstream without patterns never bypasses on path.

**Blacklist** (the default): matching paths skip the queue.

```yaml
upstreams:
  - prefix: "/api"
    url: "http://backend:3000"
    pathFilter:
      mode: "blacklist"
      patterns:
        - "^/api/auth"
        - "^/api/health"
```

Auth and health go straight to the backend. Everything else is queued.

**Whitelist**: only matching paths are queued.

```yaml
upstreams:
  - prefix: "/api"
    url: "http://backend:3000"
    pathFilter:
      mode: "whitelist"
      patterns:
        - "^/api/reports"
        - "^/api/exports"
```

Reports and exports are queued. Everything else goes straight through. Any mode other than `whitelist` acts as `blacklist`.

## How bypassed requests reach the upstream

`directProxyMode` picks how a bypassed request travels.

**`proxy`** (the default): proxq reverse-proxies the request and streams in both directions. The client never sees the upstream URL, and the response is the upstream's own, without `X-Proxq-Source`. The upstream receives the client's `Host` header. Unlike queued requests, direct-proxied requests get no `X-Forwarded-For`, `X-Real-IP` or `X-Forwarded-Proto` from proxq, and incoming `X-Forwarded-*` headers are removed. If the upstream cannot be reached, proxq answers `502` with `X-Proxq-Source: proxq`.

**`redirect`**: proxq answers `307 Temporary Redirect` with `X-Proxq-Source: proxq` and a `Location` of the upstream URL plus the stripped path and query. The client must be able to reach the upstream itself, and it re-sends the body there.

WebSocket upgrades are always reverse-proxied, whatever `directProxyMode` says.

Direct-proxied requests are not cached and are not jobs, so there is nothing to poll. proxq sets no timeout of its own on them.
