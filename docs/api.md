---
title: "Jobs API"
description: "Submit a request to proxq, read the job status, replay the stored upstream response, cancel a job, and tell proxq answers from upstream ones."
nav_order: 3
---

# Jobs API

Any request that does not hit a job endpoint is routed to an upstream and queued, unless it [skips the queue](routing.md#direct-proxy-bypass). The job endpoints live under `jobsPath`, `/__jobs` by default. There is no authentication on any of them; see [security](security.md).

| Method | Path | Does |
| --- | --- | --- |
| any | any other path | Queue the request and return a job ID |
| `GET` | `/__jobs/{id}` | Job status |
| `GET` | `/__jobs/{id}/content` | Replay the stored upstream response |
| `DELETE` | `/__jobs/{id}` | Cancel and delete the job |

## Telling proxq from the upstream

proxq sets `X-Proxq-Source: proxq` on the answers it writes itself on the request path: `202`, `400`, `500`, `502`, `307`, and on every `404` from the job endpoints. A response replayed from `/content` never carries it, even when the upstream returned `404` or `500`.

On `/content`, a `404` **with** `X-Proxq-Source` means the job is not done or does not exist, and any response **without** it is the upstream speaking.

The `200` answers from the status and cancel endpoints carry no `X-Proxq-Source`, and neither do the `500` answers those endpoints write when Redis fails.

proxq's own errors share one JSON shape:

```json
{
  "code": "NOT_FOUND",
  "message": "Not found"
}
```

The codes are `BAD_REQUEST`, `NOT_FOUND`, `INTERNAL_SERVER_ERROR` and `BAD_GATEWAY`. Every response also carries an `X-Request-Id` header, taken from the request when the client sent one and generated otherwise.

## Submit a request

Send the request you want proxied, with any method, headers and body:

```http
POST /api/heavy-computation HTTP/1.1
Content-Type: application/json
Authorization: Bearer upstream-token

{"data": "lots of it"}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
X-Proxq-Source: proxq

{
  "jobId": "550e8400-e29b-41d4-a716-446655440000"
}
```

The job ID is a random UUID. The stored request keeps the method, the URL after [prefix stripping](routing.md#prefix-stripping), every header, the client IP and scheme, and the body up to the upstream's `maxBodySize`.

| Answer | When |
| --- | --- |
| `202 Accepted` | Queued |
| `400 Bad Request` | `X-Proxq-Timeout` is not a valid duration |
| `502 Bad Gateway` | No upstream prefix matches the path |
| `500 Internal Server Error` | The body could not be read, or Redis refused the job |

### Per-request timeout

`X-Proxq-Timeout` overrides the upstream's `timeout` for one request. It takes a Go duration such as `30s` or `10m`:

```bash
curl -s -X POST localhost:8080/api/slow \
  -H 'X-Proxq-Timeout: 15m' \
  -d '{}'
```

The header is also forwarded to the upstream with the rest.

## Get the job status

```http
GET /__jobs/550e8400-e29b-41d4-a716-446655440000 HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "completedAt": "2026-01-01T00:00:00Z"
}
```

A failed job carries the last error:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "failed",
  "error": "forward request: do request: ..."
}
```

| Status | Meaning | asynq states |
| --- | --- | --- |
| `queued` | Waiting for a worker | pending, scheduled, aggregating |
| `running` | A worker has it, or a retry is pending | active, retry |
| `completed` | Done, response stored, whatever status the upstream returned | completed |
| `failed` | Forwarding broke and the retries ran out | archived |

`completedAt` appears once the job has completed. `error` holds the last error and can be present on a `running` job that is waiting for a retry. An unknown ID returns `404` with `X-Proxq-Source`.

## Get the job content

This is the payoff. proxq replays the upstream response exactly: status code, headers and body, as if you had called the upstream yourself.

```http
GET /__jobs/550e8400-e29b-41d4-a716-446655440000/content HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Custom-Header: from-upstream

{"result": "done"}
```

The content can be fetched as many times as you like until the job expires after `taskRetention`. When caching is on, the replay also carries `X-Cache-Status`; see [caching](caching.md).

A job that is not `completed`, including a `failed` one, and an unknown ID both return `404` with `X-Proxq-Source`. Check the status to tell them apart.

## Cancel a job

```http
DELETE /__jobs/550e8400-e29b-41d4-a716-446655440000 HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "status": "cancelled"
}
```

proxq first asks a worker running the job to stop, then deletes the job from Redis. The stored response goes with it, and there is no undo. An unknown ID returns `404` with `X-Proxq-Source`. There is no ownership check: anyone who can reach proxq and knows a job ID can cancel it.

asynq cannot delete a job while a worker still holds it. Cancelling a `running` job can therefore return `500` after the stop signal went out; the stopped attempt counts as a failure, so the job is retried or ends `failed`. Send the `DELETE` again once the status is no longer `running`.

## A polling loop

```bash
JOB_ID=$(
  curl -s -X POST localhost:8080/api/report -d '{}' |
    jq -r .jobId
)

while true; do
  STATUS=$(curl -s "localhost:8080/__jobs/$JOB_ID" | jq -r .status)

  case "$STATUS" in
    completed)
      curl -s "localhost:8080/__jobs/$JOB_ID/content"
      break
      ;;
    failed)
      echo "job failed" >&2
      break
      ;;
    *)
      sleep 2
      ;;
  esac
done
```

The [OpenAI Go client](openai-client.md) does the same loop inside an `http.RoundTripper`.

## Headers

### Forwarded to the upstream

Queued requests forward every original header, except the hop-by-hop headers `Connection`, `Keep-Alive`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailers`, `Transfer-Encoding` and `Upgrade`. proxq sets these:

| Header | Value |
| --- | --- |
| `X-Forwarded-For` | Client IP |
| `X-Real-IP` | Client IP, same value |
| `X-Forwarded-Proto` | An incoming `X-Forwarded-Proto` when present, else `http` (proxq itself serves plain HTTP) |

The client IP is the first address of an incoming `X-Forwarded-For`, else an incoming `X-Real-IP`, else the connection's address. proxq replaces an incoming `X-Forwarded-For` chain with that one address, and a client can set it to anything; see [security](security.md#forwarded-headers).

The `Host` header of a queued request is the upstream's host. A direct-proxied request keeps the client's `Host`; see [routing](routing.md#how-bypassed-requests-reach-the-upstream).

### Set by proxq

| Header | Value | When |
| --- | --- | --- |
| `X-Proxq-Source` | `proxq` | Answers proxq writes itself, as described [above](#telling-proxq-from-the-upstream) |
| `X-Cache-Status` | `HIT` or `MISS` | On replayed responses when caching is on |
| `X-Request-Id` | request ID | Every response |

### Read from the client

| Header | Effect |
| --- | --- |
| `X-Proxq-Timeout` | Overrides the upstream `timeout` for this request. Invalid value: `400` |
