package middleware

import (
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/psyb0t/common-go/slogging"
)

type LoggerConfig struct {
	LogLevel       slog.Level
	Message        string
	SkipPaths      map[string]bool
	ExtraFields    map[string]any
	IncludeQuery   bool
	IncludeHeaders bool
	HeaderFields   []string
}

type LoggerOption func(*LoggerConfig)

func WithLogLevel(level slog.Level) LoggerOption {
	return func(c *LoggerConfig) {
		c.LogLevel = level
	}
}

func WithLogMessage(message string) LoggerOption {
	return func(c *LoggerConfig) {
		c.Message = message
	}
}

func WithSkipPaths(paths ...string) LoggerOption {
	return func(c *LoggerConfig) {
		if c.SkipPaths == nil {
			c.SkipPaths = make(map[string]bool)
		}

		for _, path := range paths {
			c.SkipPaths[path] = true
		}
	}
}

func WithExtraFields(
	fields map[string]any,
) LoggerOption {
	return func(c *LoggerConfig) {
		if c.ExtraFields == nil {
			c.ExtraFields = make(map[string]any)
		}

		maps.Copy(c.ExtraFields, fields)
	}
}

func WithIncludeQuery(include bool) LoggerOption {
	return func(c *LoggerConfig) {
		c.IncludeQuery = include
	}
}

func WithIncludeHeaders(
	headers ...string,
) LoggerOption {
	return func(c *LoggerConfig) {
		c.IncludeHeaders = len(headers) > 0
		c.HeaderFields = headers
	}
}

//nolint:funlen
func Logger(opts ...LoggerOption) Middleware {
	config := &LoggerConfig{
		LogLevel:       slog.LevelInfo,
		Message:        "HTTP request",
		SkipPaths:      make(map[string]bool),
		ExtraFields:    make(map[string]any),
		IncludeQuery:   true,
		IncludeHeaders: false,
		HeaderFields:   []string{},
	}

	for _, opt := range opts {
		opt(config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				if config.SkipPaths[r.URL.Path] {
					next.ServeHTTP(w, r)

					return
				}

				ctx := r.Context()
				start := time.Now()

				logger := slogging.GetLogger(ctx).With(
					"method", r.Method,
					"path", r.URL.Path,
					"ip", aichteeteapee.GetClientIP(r),
				)

				ctx = slogging.GetCtxWithLogger(
					ctx, logger,
				)

				wrapped := &loggerResponseWriter{
					BaseResponseWriter: BaseResponseWriter{
						ResponseWriter: w,
					},
					statusCode: http.StatusOK,
				}

				defer func() {
					l := slogging.GetLogger(ctx).With(
						"status", wrapped.getStatusCode(),
						"duration", time.Since(start).String(),
						"userAgent", r.Header.Get(
							aichteeteapee.HeaderNameUserAgent,
						),
					)

					if config.IncludeQuery {
						l = l.With(
							"query", r.URL.RawQuery,
						)
					}

					for k, v := range config.ExtraFields {
						l = l.With(k, v)
					}

					if config.IncludeHeaders {
						for _, h := range config.HeaderFields {
							if v := r.Header.Get(h); v != "" {
								l = l.With("header_"+h, v)
							}
						}
					}

					l.Log(
						ctx,
						config.LogLevel,
						config.Message,
					)
				}()

				next.ServeHTTP(
					wrapped, r.WithContext(ctx),
				)
			},
		)
	}
}

type loggerResponseWriter struct {
	BaseResponseWriter
	statusCode    int
	mu            sync.Mutex
	headerWritten bool
}

func (rw *loggerResponseWriter) WriteHeader(
	code int,
) {
	rw.mu.Lock()

	if !rw.headerWritten {
		rw.statusCode = code
		rw.headerWritten = true
		rw.mu.Unlock()
		rw.ResponseWriter.WriteHeader(code)
	} else {
		rw.mu.Unlock()
	}
}

func (rw *loggerResponseWriter) getStatusCode() int {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	return rw.statusCode
}
