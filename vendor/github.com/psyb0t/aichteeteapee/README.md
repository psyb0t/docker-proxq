# aichteeteapee

Pronounced "HTTP". The name is the whole joke. Moving on.

A Go HTTP library that does everything you need and nothing you don't. Spin up a production-ready server with middleware, WebSocket hubs, file uploads, static serving, request proxying with caching, and OpenAPI validation — all with sane defaults and zero boilerplate.

```go
srv, _ := server.New()
srv.GetRootGroup().GET("/hello", func(w http.ResponseWriter, _ *http.Request) {
    aichteeteapee.WriteJSON(w, http.StatusOK, map[string]string{"msg": "hi"})
})
srv.Start(ctx, nil)
```

That gives you HTTP + HTTPS, graceful shutdown, structured logging, request IDs, and security headers. Out of the box.

## What's in the box

### Root package — constants and utilities

Everything you need to stop hardcoding strings in your HTTP code.

**Content types**: `ContentTypeJSON`, `ContentTypeYAML`, `ContentTypeXML`, `ContentTypeHTML`, `ContentTypeOctetStream`, `ContentTypeMultipartFormData`, `ContentTypeApplicationFormURLEncoded`, `ContentTypeTextEventStream`, and more.

**Header names**: Every header you'll ever use as a constant. `HeaderNameAuthorization`, `HeaderNameContentType`, `HeaderNameXForwardedFor`, `HeaderNameXRequestID`, all CORS headers, all security headers, all hop-by-hop headers (RFC 2616). Plus `AuthSchemeBearer` for the "Bearer " prefix.

**Error handling**: `ErrorCode` constants for every HTTP status (`ErrorCodeBadRequest`, `ErrorCodeNotFound`, etc.), `ErrorCodeFromHTTPStatus()` mapper, sentinel error vars (`ErrBadRequest`, `ErrNotFound`, etc.), and pre-built `ErrorResponse` structs ready to serialize.

**Request utilities**: `GetClientIP(r)` (respects `X-Forwarded-For` → `X-Real-IP` → `RemoteAddr`), `GetRequestID(r)`, content type checkers (`IsRequestContentTypeJSON`, etc.).

**Response utilities**: `WriteJSON(w, statusCode, data)` — pretty-printed JSON with proper headers.

**Network & scheme constants**: `SchemeHTTP`, `SchemeHTTPS`, `NetworkTypeTCP`, `NetworkTypeUnix`, etc.

### `server/` — HTTP server

Built on `net/http` with routing via Go 1.22+ `ServeMux` patterns.

```go
srv, _ := server.New()                    // defaults from env vars
srv, _ := server.NewWithConfig(config)     // explicit config
```

**Router with groups and middleware**:
```go
router := &server.Router{
    GlobalMiddlewares: []middleware.Middleware{
        middleware.RequestID(),
        middleware.Logger(),
        middleware.Recovery(),
        middleware.SecurityHeaders(),
        middleware.CORS(),
    },
    Static: []server.StaticRouteConfig{
        {Dir: "./static", Path: "/static"},
    },
    Groups: []server.GroupConfig{
        {
            Path: "/api/v1",
            Routes: []server.RouteConfig{
                {Method: http.MethodGet, Path: "/users", Handler: listUsers},
                {Method: http.MethodPost, Path: "/users", Handler: createUser},
            },
        },
        {
            Path: "/admin",
            Middlewares: []middleware.Middleware{
                middleware.BasicAuth(middleware.WithBasicAuthUsers(users)),
            },
            Routes: []server.RouteConfig{
                {Method: http.MethodGet, Path: "/stats", Handler: adminStats},
            },
        },
    },
}

srv.Start(ctx, router)
```

**Built-in handlers**: `srv.HealthHandler`, `srv.EchoHandler`, `srv.FileUploadHandler(uploadsDir)`.

**File uploads** with configurable filename prepending (none, datetime, UUID) and custom post-processors.

**Static file serving** with path traversal protection, file caching, and directory indexing (HTML or JSON).

**TLS** out of the box — set `HTTP_SERVER_TLSENABLED=true` and point to cert/key files.

**Config from environment**:

| Variable | Default |
|---|---|
| `HTTP_SERVER_LISTENADDRESS` | `127.0.0.1:8080` |
| `HTTP_SERVER_READTIMEOUT` | `15s` |
| `HTTP_SERVER_WRITETIMEOUT` | `30s` |
| `HTTP_SERVER_IDLETIMEOUT` | `60s` |
| `HTTP_SERVER_SHUTDOWNTIMEOUT` | `10s` |
| `HTTP_SERVER_MAXHEADERBYTES` | `1MB` |
| `HTTP_SERVER_TLSENABLED` | `false` |
| `HTTP_SERVER_TLSLISTENADDRESS` | `127.0.0.1:8443` |

### `server/middleware/` — middleware stack

Every middleware uses `slogging.GetLogger(ctx)` for structured logging with context propagation. RequestID sets the request ID on the context logger, Logger adds method/path/ip — downstream code gets all fields for free.

**RequestID** — generates or extracts `X-Request-ID`, sets it on the context logger, adds it to the response header.

**Logger** — structured request/response logging with configurable log level, skip paths, extra fields, query/header inclusion.

```go
middleware.Logger(
    middleware.WithLogLevel(slog.LevelDebug),
    middleware.WithSkipPaths("/health"),
    middleware.WithExtraFields(map[string]any{"service": "api"}),
    middleware.WithIncludeHeaders(aichteeteapee.HeaderNameAuthorization),
)
```

**Recovery** — catches panics, logs with stack trace, returns 500 JSON response. Fully configurable response body, status code, and content type.

**BasicAuth** — HTTP Basic Authentication with constant-time comparison, custom validators, skip paths, realm config.

**CORS** — full CORS with origin lists, methods, headers, credentials, max-age, preflight handling.

**SecurityHeaders** — `X-Content-Type-Options`, `X-Frame-Options`, `X-XSS-Protection`, HSTS, Referrer-Policy, CSP. Each individually configurable or disableable.

**Timeout** — request deadline with 504 Gateway Timeout response. Presets: `WithDefaultTimeout()`, `WithShortTimeout()`, `WithLongTimeout()`.

**EnforceRequestContentType** — reject requests with wrong `Content-Type`. Skips GET/HEAD/DELETE. Convenience: `EnforceRequestContentTypeJSON()`.

### `server/proxy/` — HTTP request forwarding

Forward requests to upstream servers with optional response caching.

```go
result, err := proxy.ForwardRequest(ctx, proxy.ForwardConfig{
    HTTPClient: http.DefaultClient,
    Cache:      myCache,            // nil = no caching
    CacheTTL:   5 * time.Minute,
    CacheKeyExcludeHeaders: map[string]struct{}{
        aichteeteapee.HeaderNameXRequestID: {},
    },
}, payload)
```

- Sets `X-Forwarded-For`, `X-Real-IP`, `X-Forwarded-Proto` on upstream requests
- Strips hop-by-hop headers per RFC 2616
- Caches 2xx responses, skips errors
- Cache key = `sha256(method + url + headers + body)` with configurable header exclusions
- Custom cache key function via `CacheKeyFn`
- Full structured logging: debug for hits/misses/forwards, warn for upstream 5xx, error for connection failures

`RequestPayload.Hash()` and `RequestPayload.HashExcluding(excludeHeaders)` for deterministic request fingerprinting.

### `server/dabluvee-es/` — WebSocket event system

Pronounced "WS" — because why stop at one wordplay.

A three-tier WebSocket architecture: **Hub** → **Client** → **Connection**.

```go
hub := wshub.NewHub("chat")

hub.RegisterEventHandler("chat.message", func(
    hub wshub.Hub, client *wshub.Client, event *dabluveees.Event,
) error {
    hub.BroadcastToAll(event)
    return nil
})

// HTTP upgrade endpoint
mux.HandleFunc("/ws/chat", wshub.UpgradeHandler(hub, clientID))
```

**Events** have UUID4 IDs, typed payloads (`json.RawMessage` — zero-copy), timestamps, and thread-safe metadata maps. Built-in types: `EventTypeSystemLog`, `EventTypeShellExec`, `EventTypeEchoRequest`/`Reply`, `EventTypeError`.

**Hubs** manage clients, route events to handlers, broadcast to all/specific/subscribed clients.

**Clients** represent logical users with multiple connections. Atomic state management, configurable buffer sizes, ping/pong heartbeat.

**Connections** wrap `gorilla/websocket` with write pumps, read pumps, graceful shutdown, and in-flight message tracking.

**Unix Socket Bridge** (`wsunixbridge`) — bridges WebSocket connections to Unix domain sockets for integrating external tools. Each connection gets dedicated reader/writer sockets.

### `echo/` — Echo framework wrapper

For when you want [labstack/echo](https://github.com/labstack/echo) instead of `net/http`.

```go
e, _ := echo.New("/api", swaggerYAML, middlewares)
e.Start(ctx)
```

Auto-serves OpenAPI spec at `OASPath` and Swagger UI at `SwaggerUIPath`. Config from `HTTP_ECHO_LISTENADDRESS` env var (defaults to `0.0.0.0:8080`).

### `echo/middleware/` — Echo API middleware

```go
middlewares := echomw.CreateDefaultAPIMiddleware(spec, echomw.BearerAuth(
    func(ctx context.Context, token string) error {
        return validateToken(ctx, token)
    },
))
```

OpenAPI request validation + panic recovery + Bearer token auth in one call.

### `oapi-codegen/middleware/` — OpenAPI validation for Echo

Wraps [oapi-codegen/echo-middleware](https://github.com/oapi-codegen/echo-middleware) with structured error responses using `aichteeteapee.ErrorResponse`.

```go
mw := oapimw.OapiValidatorMiddleware(spec)
// or with options:
mw := oapimw.OapiValidatorMiddlewareWithOptions(spec, opts)
```

## Logging

All middleware and proxy code uses [common-go/slogging](https://github.com/psyb0t/common-go) for context-propagated structured logging.

The middleware chain builds up the logger progressively:
1. **RequestID** adds `requestId` to the context logger
2. **Logger** adds `method`, `path`, `ip`
3. **Any downstream code** calls `slogging.GetLogger(ctx)` and gets all fields automatically

No explicit logger passing needed. Every log line from every middleware and handler automatically includes the full request context.

## Development

```bash
make dep            # go mod tidy + vendor
make lint           # golangci-lint (strict)
make lint-fix       # lint + auto-fix
make test           # go test -race ./...
make test-coverage  # coverage with 85% minimum threshold
```

Smoke tests in `server/.test-NOT PART OF PROJECT/`:
```bash
cd "server/.test-NOT PART OF PROJECT"
bash run_tests_rapid.sh
```

Covers: health, API, static files, middleware, WebSocket stats, directory security, proxy forwarding, cache hits, logging context propagation, request ID headers, and log field verification.

## License

MIT. See [LICENSE](LICENSE).
