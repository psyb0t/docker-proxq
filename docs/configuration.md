---
title: "Configuration"
description: "Every proxq config field with its default: listen address, Redis, queue, workers, jobs path, retention, cache, upstreams and logging."
nav_order: 2
---

# Configuration

proxq reads one YAML file at startup. Changes need a restart. [`config.yaml.example`](https://github.com/psyb0t/docker-proxq/blob/main/config.yaml.example) in the repository lists every field.

## Config file path

The path is the first of these that is set:

1. the `--config` flag, as in `proxq --config /etc/proxq/config.yaml`;
2. the `PROXQ_CONFIG` environment variable;
3. `config.yaml` in the working directory, which is `/app` in the image.

Nothing else is read from the environment except the [logging variables](#logging). There is no per-field environment override, so mount or generate the YAML file.

## Minimal config

At least one upstream is required. Everything else has a default.

```yaml
listenAddress: "0.0.0.0:8080"

redis:
  addr: "redis:6379"

upstreams:
  - prefix: "/"
    url: "http://your-api:3000"
```

The default `listenAddress` is `127.0.0.1:8080`, which nothing outside the container can reach. Set `0.0.0.0:8080` when proxq runs in Docker.

## Global settings

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `listenAddress` | string | `127.0.0.1:8080` | HTTP bind address |
| `redis.addr` | string | `127.0.0.1:6379` | Redis server address, used by the queue and the `redis` cache |
| `redis.username` | string | `""` | Redis ACL username. Empty authenticates as the default user with the password alone |
| `redis.password` | string | `""` | Redis password |
| `redis.db` | int | `0` | Redis database number |
| `queue` | string | `default` | asynq queue name |
| `concurrency` | int | `10` | Number of workers forwarding requests at the same time |
| `jobsPath` | string | `/__jobs` | Base path of the [jobs API](api.md) |
| `taskRetention` | duration | `1h` | How long a completed or failed job stays in Redis |
| `cache` | object | mode `none` | See [cache settings](#cache-settings) |
| `upstreams` | list | required | See [upstream settings](#upstream-settings) |

Durations use Go syntax: `30s`, `5m`, `1h`, `1h30m`. A value such as `1 hour` stops startup with a parse error.

A field set to `0` or `""` gets its default, because proxq treats a zero value as "not set".

### Redis ACL users

proxq can run as its own Redis ACL user instead of the `default` user. Set `redis.username` and `redis.password`. A least-privilege user needs asynq's keys and channels plus the cache prefix:

```
user proxq on >secret ~asynq:* ~proxq:* &asynq:* +@all
```

`~proxq:*` matches the default `cache.redisKeyPrefix`. Change it if you change the prefix.

## Upstream settings

Each entry in `upstreams` routes one path prefix to one backend. The [routing](routing.md) page explains matching and stripping.

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `prefix` | string | required | Path prefix to match. Stripped before forwarding |
| `url` | string | required | Upstream base URL. May include a path, such as `http://api:3000/v2` |
| `timeout` | duration | `5m` | Time limit for one forwarding attempt. A client can override it per request with `X-Proxq-Timeout` |
| `maxRetries` | int | `0` | Retries after a transport failure. `0` means none |
| `retryDelay` | duration | `0` | Fixed delay between retries. `0` means exponential backoff |
| `maxBodySize` | int (bytes) | `10485760` (10 MiB) | Most bytes of a request body stored in a job. Longer bodies are cut to this size |
| `directProxyThreshold` | int (bytes) | `10485760` (10 MiB) | A request with a larger `Content-Length` skips the queue. A negative value turns this check off |
| `directProxyMode` | string | `proxy` | How bypassed requests reach the upstream: `proxy` or `redirect` |
| `cacheKeyExcludeHeaders` | list | `[]` | Headers left out of the cache key. Empty uses the defaults on the [caching](caching.md#cache-key) page |
| `pathFilter.mode` | string | `blacklist` | `blacklist`: matching paths skip the queue. `whitelist`: only matching paths are queued |
| `pathFilter.patterns` | list | `[]` | Go regular expressions matched against the full request path, before stripping |

Two of these behave differently from what their names suggest:

- `directProxyThreshold: 0` does not turn the size check off. Zero means "not set" and becomes 10 MiB. Use `-1` to queue every body regardless of size.
- `maxBodySize` truncates. A queued body longer than the limit is cut to `maxBodySize` bytes and forwarded without an error. Keep `directProxyThreshold` at or below `maxBodySize`, so a body that would be cut goes direct instead.

`directProxyMode` is not validated. Any value other than `redirect` behaves like `proxy`.

### Retries

A job fails only when forwarding itself breaks: a connection error, a timeout, or an upstream response over 100 MiB. An upstream `500` is a completed job, and its response is stored like any other.

With `maxRetries` above zero, a failed attempt is retried. `retryDelay` sets a fixed wait. Without it, the wait is `n^4` seconds, where `n` counts the retries already made, so the first retry has no delay:

| Retry | Wait |
| --- | --- |
| 1st | none |
| 2nd | 1 second |
| 3rd | 16 seconds |
| 4th | 81 seconds |
| 5th | about 4 minutes |

While a retry is pending the job status is `running`. When the retries run out it is `failed`.

## Cache settings

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `cache.mode` | string | `none` | `none`, `memory` (in-process LRU) or `redis` (shared, same Redis as the queue) |
| `cache.ttl` | duration | `5m` | How long a cached response stays fresh |
| `cache.maxEntries` | int | `10000` | Entry limit of the `memory` cache |
| `cache.redisKeyPrefix` | string | `proxq:` | Key prefix of the `redis` cache |

Any other mode stops startup with `invalid cache mode`. The [caching](caching.md) page covers what is cached and how the key is built.

## Validation

proxq refuses to start when:

- `upstreams` is empty;
- an upstream has no `prefix` or no `url`;
- a `pathFilter` pattern is not a valid regular expression;
- an upstream prefix equals `jobsPath`, is a parent path of it, or sits under it;
- there are several upstreams and one of them has the prefix `/`;
- two prefixes nest, such as `/api` and `/api/v2`.

A single upstream may use `/` as a catch-all. The exact error messages are on the [troubleshooting](troubleshooting.md#startup-errors) page.

## Logging

Logging is configured from the environment, not the YAML file:

| Variable | Default | Values |
| --- | --- | --- |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `text` | `text`, `json` |
| `LOG_ADD_SOURCE` | `false` | `true` or `false`: add the source file and line to each record |

An invalid value makes the binary panic at startup. `debug` logs each enqueue, bypass and forwarded request with its URL.

## Full example

```yaml
listenAddress: "0.0.0.0:8080"

redis:
  addr: "redis:6379"
  username: ""
  password: ""
  db: 0

queue: "default"
concurrency: 10
jobsPath: "/__jobs"
taskRetention: "1h"

cache:
  mode: "redis"
  ttl: "10m"
  redisKeyPrefix: "proxq:"

upstreams:
  - prefix: "/api"
    url: "http://api-server:3000"
    timeout: "5m"
    maxRetries: 3
    retryDelay: "10s"
    pathFilter:
      mode: "blacklist"
      patterns:
        - "^/api/auth"
        - "^/api/health"

  - prefix: "/uploads"
    url: "http://file-server:9000/storage"
    timeout: "10m"
    maxBodySize: 1073741824
    directProxyThreshold: -1
```

The `/uploads` upstream queues bodies of any size up to 1 GiB, because its threshold check is off.
