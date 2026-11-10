---
title: "Troubleshooting"
description: "proxq startup errors with their exact messages and fixes, plus runtime problems: 500 on submit, 502, jobs stuck queued, cache misses."
nav_order: 10
---

# Troubleshooting

## Startup errors

proxq logs one `run error` line and exits with status 1 when the config is unusable. The line looks like this, followed by source locations in square brackets:

```
level=ERROR msg="run error" error="parse config: at least one upstream required [...]"
```

Search for the text after `parse config:` below.

| Message | Cause | Fix |
| --- | --- | --- |
| `read config file: open config.yaml: no such file or directory` | No file at the [config path](configuration.md#config-file-path) | Mount the file and set `PROXQ_CONFIG` or `--config` |
| `parse config yaml: ...` | The file is not valid YAML, or a field has the wrong type | Fix the YAML at the line the message names |
| `parse duration: 1 hour: time: unknown unit " hour" ...` | A duration is not in Go syntax | Use `1h`, `30s`, `1h30m` |
| `at least one upstream required` | `upstreams` is missing or empty | Add an upstream |
| `upstream prefix: required field is not set` | An upstream has no `prefix` | Add the prefix |
| `upstream url for prefix: /a: required field is not set` | The upstream for `/a` has no `url` | Add the URL |
| `compile path filter for prefix: /a: compile regex: (: error parsing regexp: ...` | A `pathFilter` pattern is not a valid Go regular expression | Fix the pattern |
| `/__jobs conflicts with /__jobs: upstream prefix conflicts with jobs path` | An upstream prefix equals `jobsPath`, is a parent of it, or sits under it | Change the prefix or `jobsPath` |
| `root prefix "/" only allowed with a single upstream` | `/` is used next to other upstreams | Give every upstream its own prefix |
| `/api/v2 overlaps /api: upstream prefixes must not overlap` | Two prefixes nest | Use prefixes that do not contain each other |
| `create cache: unknown cache mode: lru: invalid cache mode` | `cache.mode` is not `none`, `memory` or `redis` | Fix the mode |

A bad logging variable stops the binary earlier, with a panic instead of a `run error` line:

```
panic: failed to get log level: loud: invalid log level
```

Fix `LOG_LEVEL`, `LOG_FORMAT` or `LOG_ADD_SOURCE`; see [logging](configuration.md#logging).

## Every submit returns 500

proxq starts even when Redis is unreachable. It then answers every submit with `500 Internal Server Error` and `X-Proxq-Source: proxq`, the job endpoints answer `500` too, and the log fills with lines such as:

```
redis: connection pool: failed to dial after 5 attempts: dial tcp 127.0.0.1:6379: connect: connection refused
```

Check `redis.addr`, and `redis.username` and `redis.password` if the server uses ACLs. Inside Docker, `127.0.0.1` is the proxq container itself, so use the Redis service name.

## 502 Bad Gateway

With `X-Proxq-Source: proxq` and the log line `no upstream match`, the path matched no upstream prefix. Remember that `/api` does not match `/api2`; see [prefix matching](routing.md#prefix-matching).

With `X-Proxq-Source: proxq` and the log line `reverse proxy error`, a direct-proxied request could not reach the upstream. Check the upstream `url` from inside the proxq container.

## Nothing answers on the port

The default `listenAddress` is `127.0.0.1:8080`, which is unreachable from outside the container. Set `listenAddress: "0.0.0.0:8080"` in the config.

## Jobs stay queued

Workers only take jobs from the queue named in `queue`. Two proxq instances with different `queue` values on the same Redis do not take each other's jobs. Also check `concurrency`: with every worker busy on a slow upstream, new jobs wait.

## Content returns 404 with X-Proxq-Source

The job is not `completed` yet, it `failed`, it was cancelled, or it expired after `taskRetention`. Read `GET /__jobs/{id}` to see which.

## A job failed

The status `error` field holds the last error. Common ones:

- `dial tcp ...: connection refused`: the upstream is down or the `url` is wrong;
- `context deadline exceeded`: the attempt ran past `timeout` or `X-Proxq-Timeout`;
- `upstream response body exceeds maximum size`: the response was over 100 MiB.

Set `maxRetries` to retry transport failures; see [retries](configuration.md#retries).

## The cache never hits

- Only `2xx` responses are cached.
- The key includes every header not excluded. A header with a new value per request, such as a request ID or a trace ID, makes every key unique. Exclude it with `cacheKeyExcludeHeaders`, in canonical form; see [cache key](caching.md#cache-key).
- `memory` mode is per process and starts empty after a restart.

## The upstream receives a truncated body

The body was longer than `maxBodySize` and `directProxyThreshold` was off or above it. Keep `directProxyThreshold` at or below `maxBodySize`; see [upstream settings](configuration.md#upstream-settings).
