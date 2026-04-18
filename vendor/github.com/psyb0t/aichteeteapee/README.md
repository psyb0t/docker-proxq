# aichteeteapee

Pronounced "HTTP". The name is the whole joke. Moving on.

A Go HTTP library that does everything you need and nothing you don't. Spin up a production-ready server with middleware, WebSocket hubs, file uploads, static serving, request proxying with caching, and OpenAPI validation — all with secure defaults and zero boilerplate.

```go
srv, _ := serbewr.New()

router := &serbewr.Router{
    GlobalMiddlewares: []middleware.Middleware{
        middleware.RequestID(),
        middleware.Logger(),
        middleware.Recovery(),
        middleware.SecurityHeaders(),
        middleware.CORS(),
    },
    Groups: []serbewr.GroupConfig{{
        Path: "/",
        Routes: []serbewr.RouteConfig{{
            Method:  http.MethodGet,
            Path:    "/hello",
            Handler: func(w http.ResponseWriter, _ *http.Request) {
                aichteeteapee.WriteJSON(w, http.StatusOK, map[string]string{"msg": "hi"})
            },
        }},
    }},
}

srv.Start(ctx, router)
```

## Security

Secure by default. CORS blocks unknown origins, WebSocket validates `Origin` against `Host`, file uploads sanitize filenames (no path traversal), uploaded files get `0600` permissions and won't overwrite existing files, request IDs are validated, proxy responses are size-limited, sensitive headers are filtered from echo responses.

Need to skip all that during local dev? One call:

```go
aichteeteapee.FuckSecurity()
```

CORS allows all origins, WebSocket accepts any origin. Call `aichteeteapee.UnfuckSecurity()` to go back.

Per-component overrides without global dev mode:

```go
middleware.CORS(middleware.WithAllowAllOrigins())

wshub.UpgradeHandler(hub,
    wshub.WithUpgradeHandlerCheckOrigin(
        aichteeteapee.GetPermissiveWebSocketCheckOrigin,
    ),
)
```

## What's in the box

### Root package — constants and utilities

Everything you need to stop hardcoding strings in your HTTP code.

- **Content types** — `ContentTypeJSON`, `ContentTypeYAML`, `ContentTypeHTML`, `ContentTypeMultipartFormData`, etc.
- **Header names** — every header as a constant: authentication, content negotiation, CORS, cache, security, hop-by-hop, rate limiting, WebSocket. Plus `AuthSchemeBearer` and `AuthSchemeBasic` for scheme prefixes.
- **Error handling** — `ErrorCode` constants for every HTTP status, `ErrorCodeFromHTTPStatus()` mapper, sentinel errors (`ErrBadRequest`, `ErrNotFound`, ...), pre-built `ErrorResponse` structs.
- **Request utilities** — `GetClientIP(r)`, `GetRequestID(r)`, content type checkers (`IsRequestContentTypeJSON`, etc.).
- **Response utilities** — `WriteJSON(w, statusCode, data)`.
- **Network constants** — `SchemeHTTP`, `SchemeHTTPS`, `NetworkTypeTCP`, `NetworkTypeUnix`, etc.

### [`serbewr/`](docs/server.md) — HTTP server

Pronounced "server". Built on `net/http` with Go 1.22+ `ServeMux` routing, grouped routes with per-group middleware, built-in handlers (health, echo, file upload), static file serving with directory indexing, TLS, and env-based config.

### [`serbewr/middleware/`](docs/middleware.md) — middleware stack

RequestID, Logger, Recovery, BasicAuth, CORS, SecurityHeaders, Timeout, EnforceRequestContentType. All use context-propagated structured logging.

### [`serbewr/prawxxey/`](docs/proxy.md) — HTTP request forwarding

Pronounced "proxy". Forward requests upstream with optional response caching, hop-by-hop header stripping, response size limits, and deterministic request fingerprinting.

### [`serbewr/dabluvee-es/`](docs/websocket.md) — WebSocket event system

Pronounced "WS". Three-tier architecture (Hub -> Client -> Connection) with typed events, handler registration, broadcast, and a Unix socket bridge for external tool integration.

### [`echo/`](docs/echo.md) — Echo framework integration

Echo wrapper with auto-served OpenAPI specs and Swagger UI. Includes Bearer auth middleware and OpenAPI request validation via oapi-codegen.

## Logging

All middleware and proxy code uses [common-go/slogging](https://github.com/psyb0t/common-go) for context-propagated structured logging.

The middleware chain builds up the logger progressively:
1. **RequestID** adds `requestId` to the context logger
2. **Logger** adds `method`, `path`, `ip`
3. Downstream code calls `slogging.GetLogger(ctx)` and gets all fields automatically

## Development

```bash
make dep            # go mod tidy + vendor
make lint           # golangci-lint (strict)
make lint-fix       # lint + auto-fix
make test           # go test -race ./...
make test-coverage  # coverage with minimum threshold
```

## License

MIT. See [LICENSE](LICENSE).
