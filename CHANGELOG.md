# Changelog

## v0.8.0 — 2026-04-24

### Fixed
- **Upstream timeout was silently ignored.** The worker's `http.Client.Timeout` was hardcoded to 5 minutes regardless of what was set in per-upstream config. Requests hit exactly 5m every time. Root cause: `WorkerConfig.UpstreamTimeout` was never passed in `app.go`, falling back to `defaultUpstreamTimeout = 5 * time.Minute`. Fix: remove the fallback, default `http.Client.Timeout` to 0, rely on the asynq task timeout (already wired correctly per-upstream) cancelling via context through `http.NewRequestWithContext`.

### Changed
- Go module renamed from `github.com/psyb0t/proxq` to `github.com/psyb0t/docker-proxq`.

### Added
- `TestWorker_ProcessTask_ContextDeadlineKillsRequest`: slow upstream, no `http.Client.Timeout`, context deadline fires at ~200ms.
- `TestWorker_ProcessTask_UpstreamTimeoutKillsRequest`: slow upstream, explicit `UpstreamTimeout`, fires at ~200ms.

---

## v0.7.0 — 2026-04-21

### Changed
- Module name corrected in `go.mod` and all import paths (was mismatched after initial rename).

---

## v0.6.0 — 2026-04-18

### Added
- **Per-upstream `cacheKeyExcludeHeaders`**: configure which request headers are excluded from the cache key hash per upstream. Non-empty list replaces the defaults entirely; empty list keeps defaults (`X-Request-ID`, `X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto`).
- Stored in `taskEnvelope`, applied per-task in worker via `buildExcludeHeaders()`.
- `TestHandler_CacheKeyExcludeHeaders` integration test.
- `TestBuildExcludeHeaders` table-driven unit test (4 cases).
- Config parse tests for `cacheKeyExcludeHeaders`.

### Changed
- Handler: warn log on no upstream match; debug log on successful enqueue (job ID, upstream, method, URL).
- Worker: debug log on task pickup and completion (method, URL, status code).
- `buildEnvelope()` extracted from `enqueueRequest()` to satisfy funlen lint limit.
- README fully rewritten with complete API reference, config tables, routing rules, header reference.

---

## v0.5.1 — 2026-04-18

### Changed
- All error assertions in tests switched from string matching to `errors.Is` with sentinels or `require.Error`.
- Additional OpenAI client unit tests covering constructor and error paths.

---

## v0.5.0 — 2026-04-18

### Added
- **Per-upstream `maxRetries` and `retryDelay`** with configurable retry delay function (`RetryDelayFunc`).
- `taskEnvelope` payload format wrapping `RequestPayload` + `RetryDelay` (replaces bare payload).
- `X-Proxq-Source: proxq` header on all proxq-generated responses (accepted, errors, redirects, job endpoints). Never set on responses proxied from upstream.
- **`pkg/clients/openai`**: drop-in replacement for the `openai-go` SDK. Swap one line — all SDK calls (chat completions, embeddings, images, audio) route through the proxq async queue transparently.

---

## v0.4.1 — 2026-04-18

### Fixed
- Lint errors.

---

## v0.4.0 — 2026-04-18

### Added
- **YAML config file** (`PROXQ_CONFIG` env var or `--config` flag) replacing individual env vars for upstream configuration.
- **Multi-upstream routing**: define multiple upstreams, each with its own prefix, URL, timeout, and rules.
- Per-upstream `directProxyThreshold`: response body size below which sync/direct proxy is used instead of queuing.
- Per-upstream `directProxyMode`: `proxy` (reverse proxy, default) or `redirect` (307 to upstream URL).
- Per-upstream `maxBodySize`: maximum accepted request body size.

---

## v0.3.0 — 2026-04-18

### Added
- **Path filter** with `blacklist` and `whitelist` modes per upstream.
  - Blacklist (default): matching paths bypass the queue and go direct.
  - Whitelist: only matching paths get queued; everything else bypasses.
- Replaced `PROXQ_DIRECT_PROXY_PATHS` with `PROXQ_PATH_FILTER` + `PROXQ_PATH_FILTER_MODE`.

---

## v0.2.0 — 2026-04-18

### Added
- **Direct proxy mode switch**: responses below `directProxyThreshold` are returned synchronously instead of being queued. Configurable via `directProxyMode` (`proxy` or `redirect`).
- e2e test suite refactored into focused test files (`jobs_test.go`, `cache_test.go`, `content_test.go`, `edge_cases_test.go`, `bypass_test.go`).
- Upstream fixture server (`tests/.fixtures/upstream.js`).

---

## v0.1.0 — 2026-04-18

Initial release.

### Features
- HTTP reverse proxy with async queue via Redis + asynq.
- Requests accepted immediately with `202 Accepted` and a job ID.
- Job status polling endpoint.
- Response caching with configurable TTL and Redis key prefix.
- Regex-based upstream URL routing.
- Concurrency control via asynq worker pool.
- Docker Compose setup with Redis.
