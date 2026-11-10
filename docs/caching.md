---
title: "Caching"
description: "proxq response caching: memory or Redis mode, TTL, which responses are stored, how the cache key is built, and the X-Cache-Status header."
nav_order: 5
---

# Caching

Hitting the same endpoint twice is for people who enjoy watching paint dry. With caching on, a queued request that matches an earlier one gets the stored response without another trip to the upstream. It still becomes a job: the cache sits in the worker, between the queue and the upstream.

## Turn it on

```yaml
cache:
  mode: "redis"
  ttl: "10m"
  redisKeyPrefix: "proxq:"
```

| Mode | Where entries live |
| --- | --- |
| `none` | Nowhere. The default |
| `memory` | An LRU inside the proxq process, capped at `cache.maxEntries` (default `10000`). Lost on restart, not shared between replicas |
| `redis` | The same Redis server as the queue, under `cache.redisKeyPrefix` (default `proxq:`). Shared by every proxq pointed at that Redis |

Entries expire after `cache.ttl`, `5m` by default. The fields are listed on the [configuration](configuration.md#cache-settings) page.

## What gets cached

- Any method. The same `POST` with the same body is a hit, a different body is a miss.
- Only `2xx` responses. A `4xx` or `5xx` is stored as the job result but not cached.
- Only queued requests. [Direct-proxied](routing.md#direct-proxy-bypass) requests never touch the cache.

The replayed response from `/content` carries `X-Cache-Status: HIT` when it came from the cache and `X-Cache-Status: MISS` when the upstream answered. With caching off there is no such header.

## Cache key

The key is a SHA-256 over the method, the full upstream URL, the request headers and the body. Headers in the exclude list are left out, so a header that changes on every request does not defeat the cache.

The default exclude list is `X-Request-ID`, `X-Forwarded-For`, `X-Real-IP` and `X-Forwarded-Proto`. Set `cacheKeyExcludeHeaders` on an upstream to replace it:

```yaml
upstreams:
  - prefix: "/api"
    url: "http://backend:3000"
    cacheKeyExcludeHeaders:
      - "X-Request-Id"
      - "X-Trace-Id"
```

The list **replaces** the defaults. Only the headers you name are excluded.

Names are compared exactly against Go's canonical header form, in which each word starts with a capital and the rest is lower case: `X-Request-Id`, `X-Real-Ip`, `X-Trace-Id`. A name written another way, such as `X-Trace-ID` or `x-trace-id`, never matches. Two of the defaults, `X-Request-ID` and `X-Real-IP`, are not in canonical form, so those two headers still change the key unless you list `X-Request-Id` and `X-Real-Ip` yourself.

`Authorization` is part of the key unless you exclude it. Leave it in when different callers must not see each other's responses.
