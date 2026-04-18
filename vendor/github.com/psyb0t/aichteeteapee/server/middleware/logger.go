package middleware

import (
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/psyb0t/aichteeteapee"
)

// LoggerConfig holds configuration for logger middleware.
type LoggerConfig struct {
	Logger         *slog.Logger
	LogLevel       slog.Level
	Message        string
	SkipPaths      map[string]bool
	ExtraFields    map[string]any
	IncludeQuery   bool
	IncludeHeaders bool
	HeaderFields   []string
}

type LoggerOption func(*LoggerConfig)

// WithLogger sets the logger instance.
func WithLogger(logger *slog.Logger) LoggerOption {
	return func(c *LoggerConfig) {
		c.Logger = logger
	}
}

// WithLogLevel sets the log level for requests.
func WithLogLevel(level slog.Level) LoggerOption {
	return func(c *LoggerConfig) {
		c.LogLevel = level
	}
}

// WithLogMessage sets the log message.
func WithLogMessage(message string) LoggerOption {
	return func(c *LoggerConfig) {
		c.Message = message
	}
}

// WithSkipPaths sets paths to skip logging.
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

// WithExtraFields adds extra fields to all log entries.
func WithExtraFields(fields map[string]any) LoggerOption {
	return func(c *LoggerConfig) {
		if c.ExtraFields == nil {
			c.ExtraFields = make(map[string]any)
		}

		maps.Copy(c.ExtraFields, fields)
	}
}

// WithIncludeQuery enables/disables query parameter logging.
func WithIncludeQuery(include bool) LoggerOption {
	return func(c *LoggerConfig) {
		c.IncludeQuery = include
	}
}

// WithIncludeHeaders enables header logging.
func WithIncludeHeaders(headers ...string) LoggerOption {
	return func(c *LoggerConfig) {
		c.IncludeHeaders = len(headers) > 0
		c.HeaderFields = headers
	}
}

// LoggerMiddleware logs HTTP requests with structured logging and configurable
// options
//
//nolint:funlen // Long function due to comprehensive logging configuration
func Logger(opts ...LoggerOption) Middleware {
	config := &LoggerConfig{
		Logger:         slog.Default(),
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
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging for specified paths
			if config.SkipPaths[r.URL.Path] {
				next.ServeHTTP(w, r)

				return
			}

			ctx := r.Context()
			start := time.Now()

			// Capture response status
			wrapped := &loggerResponseWriter{
				BaseResponseWriter: BaseResponseWriter{ResponseWriter: w},
				statusCode:         http.StatusOK,
			}

			defer func() {
				duration := time.Since(start)
				reqID := aichteeteapee.GetRequestID(r)
				clientIP := aichteeteapee.GetClientIP(r)

				args := []any{
					"method", r.Method,
					"path", r.URL.Path,
					"status", wrapped.getStatusCode(),
					"duration", duration.String(),
					"ip", clientIP,
					"userAgent", r.Header.Get(
						aichteeteapee.HeaderNameUserAgent,
					),
					"requestId", reqID,
				}

				if config.IncludeQuery {
					args = append(args, "query", r.URL.RawQuery)
				}

				for k, v := range config.ExtraFields {
					args = append(args, k, v)
				}

				if config.IncludeHeaders {
					for _, header := range config.HeaderFields {
						if value := r.Header.Get(header); value != "" {
							args = append(args, "header_"+header, value)
						}
					}
				}

				config.Logger.Log(
					ctx,
					config.LogLevel,
					config.Message,
					args...,
				)
			}()

			next.ServeHTTP(wrapped, r)
		})
	}
}

// loggerResponseWriter wraps http.ResponseWriter to capture status code safely.
type loggerResponseWriter struct {
	BaseResponseWriter
	statusCode    int
	mu            sync.Mutex
	headerWritten bool
}

func (rw *loggerResponseWriter) WriteHeader(code int) {
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
