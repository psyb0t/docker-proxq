---
title: "Security"
description: "What a proxq instance exposes: no built-in auth, outbound requests on the caller's behalf, unowned job IDs, trusted forwarded headers, and what to put in front."
nav_order: 9
---

# Security

proxq is a building block for a private network. It has no authentication, and anyone who can reach it can make it send requests to your upstreams. Plan the deployment around that.

## No authentication

No endpoint checks who is calling: not submission, not status, not content, not cancel. Anyone who can reach the listen address can:

- send any method, headers and body to every configured upstream;
- read the status and the stored response of any job whose ID they know;
- cancel any job whose ID they know.

Job IDs are random UUIDs, so they are not guessable, but whoever sees one (in a log, a URL, a shared screen) can use it. There is no ownership check.

Before exposing proxq:

- bind it to loopback or an internal network. The default `listenAddress` is `127.0.0.1:8080`; in Docker, publish the port on `127.0.0.1` as the [quick start](index.md#quick-start) does;
- if clients outside that network need it, put a reverse proxy in front that does the authentication (basic auth, OAuth2 proxy, mTLS, an API gateway).

## Outbound requests

proxq exists to make HTTP requests on someone else's behalf. The client picks the method, path, query, headers and body; the config picks the host. Point upstreams only at services you would let every proxq client call directly. An upstream at an internal admin API gives that API to anyone who can reach proxq.

`directProxyMode: redirect` sends the upstream URL to the client in the `Location` header, so the upstream address is no longer hidden.

## Stored data

Every queued request, with its headers and body, sits in Redis until a worker takes it, and the upstream response stays there for `taskRetention` (default `1h`) after the job ends. That includes `Authorization` headers, API keys and request bodies. With `cache.mode: redis`, cached responses sit there for `cache.ttl` as well.

Treat the Redis server as holding secrets: keep it on a private network, require a password, and use an ACL user for proxq as shown on the [configuration](configuration.md#redis-acl-users) page.

## Forwarded headers

proxq trusts what the client says about itself. On queued requests, `X-Forwarded-For` and `X-Real-IP` are set to the first address of the incoming `X-Forwarded-For`, else the incoming `X-Real-IP`, else the connection address. `X-Forwarded-Proto` keeps an incoming value. A client can put anything in these, so an upstream that makes access decisions from them must only be reachable through a proxy that overwrites them.

## Limits

| Limit | Value | At the limit |
| --- | --- | --- |
| Queued request body | `maxBodySize`, 10 MiB by default | The body is cut to the limit and forwarded without an error |
| Upstream response | 100 MiB, fixed | The attempt fails and counts toward retries |
| Request headers | 1 MiB, Go's default | The request is refused |
| Forwarding attempt | upstream `timeout`, `5m` by default, or `X-Proxq-Timeout` | The attempt fails |

proxq sets no read or write timeouts on its HTTP server and no timeout on direct-proxied requests. A reverse proxy in front is the place for connection limits and timeouts.

## The image

The image runs the binary as the non-root user `proxq` on `alpine:3.24`. Both base images are pinned by digest in the [`Dockerfile`](https://github.com/psyb0t/docker-proxq/blob/main/Dockerfile). CI scans the published image with Grype and the Go code with govulncheck.
