package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"runtime/debug"

	"github.com/psyb0t/aichteeteapee"
)

// RecoveryConfig holds configuration for recovery middleware.
type RecoveryConfig struct {
	Logger        *slog.Logger
	LogLevel      slog.Level
	LogMessage    string
	StatusCode    int
	Response      any
	ContentType   string
	IncludeStack  bool
	ExtraFields   map[string]any
	CustomHandler func(recovered any, w http.ResponseWriter, r *http.Request)
}

type RecoveryOption func(*RecoveryConfig)

// WithRecoveryLogger sets the logger instance.
func WithRecoveryLogger(logger *slog.Logger) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.Logger = logger
	}
}

// WithRecoveryLogLevel sets the log level for panic recovery.
func WithRecoveryLogLevel(level slog.Level) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.LogLevel = level
	}
}

// WithRecoveryLogMessage sets the log message for panic recovery.
func WithRecoveryLogMessage(message string) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.LogMessage = message
	}
}

// WithRecoveryStatusCode sets the HTTP status code for panic responses.
func WithRecoveryStatusCode(code int) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.StatusCode = code
	}
}

// WithRecoveryResponse sets the response body for panic responses.
func WithRecoveryResponse(response any) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.Response = response
	}
}

// WithRecoveryContentType sets the content type for panic responses.
func WithRecoveryContentType(contentType string) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.ContentType = contentType
	}
}

// WithIncludeStack enables/disables stack trace inclusion in logs.
func WithIncludeStack(include bool) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.IncludeStack = include
	}
}

// WithRecoveryExtraFields adds extra fields to panic log entries.
func WithRecoveryExtraFields(fields map[string]any) RecoveryOption {
	return func(c *RecoveryConfig) {
		if c.ExtraFields == nil {
			c.ExtraFields = make(map[string]any)
		}

		maps.Copy(c.ExtraFields, fields)
	}
}

// WithCustomRecoveryHandler sets a custom handler for panic recovery.
func WithCustomRecoveryHandler(
	handler func(recovered any, w http.ResponseWriter, r *http.Request),
) RecoveryOption {
	return func(c *RecoveryConfig) {
		c.CustomHandler = handler
	}
}

// RecoveryMiddleware recovers from panics with configurable options
//
// Complex panic handling logic is necessary for proper recovery
//
//nolint:gocognit,nestif,cyclop,funlen
func Recovery(opts ...RecoveryOption) Middleware {
	config := &RecoveryConfig{
		Logger:        slog.Default(),
		LogLevel:      slog.LevelError,
		LogMessage:    "Panic recovered in HTTP handler",
		StatusCode:    http.StatusInternalServerError,
		Response:      aichteeteapee.ErrorResponseInternalServerError,
		ContentType:   aichteeteapee.ContentTypeJSON,
		IncludeStack:  true, // Enable stack traces by default
		ExtraFields:   make(map[string]any),
		CustomHandler: nil,
	}

	for _, opt := range opts {
		opt(config)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			defer func() {
				if recovered := recover(); recovered != nil {
					// Use custom handler if provided
					if config.CustomHandler != nil {
						config.CustomHandler(recovered, w, r)

						return
					}

					reqID := aichteeteapee.GetRequestID(r)

					// Build log args
					args := []any{
						"error", recovered,
						"method", r.Method,
						"path", r.URL.Path,
						"ip", aichteeteapee.GetClientIP(r),
						"requestId", reqID,
					}

					// Add extra fields
					for k, v := range config.ExtraFields {
						args = append(args, k, v)
					}

					if config.IncludeStack {
						args = append(args, "stack", string(debug.Stack()))
					}

					config.Logger.Log(
						ctx,
						config.LogLevel,
						config.LogMessage,
						args...,
					)

					// Set content type if not already set
					ctHeader := aichteeteapee.HeaderNameContentType
					if w.Header().Get(ctHeader) == "" {
						w.Header().Set(
							ctHeader,
							config.ContentType,
						)
					}

					w.WriteHeader(config.StatusCode)

					// Handle JSON response encoding safely
					isJSON := config.ContentType ==
						aichteeteapee.ContentTypeJSON
					if isJSON {
						jsonData, err := json.Marshal(
							config.Response,
						)
						if err != nil {
							config.Logger.Log(
								ctx,
								config.LogLevel,
								fmt.Sprintf(
									"Failed to encode error "+
										"response during "+
										"panic recovery: %v",
									err,
								),
							)

							fallback := []byte(
								`{"code":"INTERNAL_SERVER` +
									`_ERROR","message":` +
									`"Internal server ` +
									`error"}`,
							)
							if _, wErr := w.Write(
								fallback,
							); wErr != nil {
								config.Logger.Log(
									ctx,
									config.LogLevel,
									fmt.Sprintf(
										"Failed to write "+
											"fallback response"+
											": %v",
										wErr,
									),
								)
							}
						} else {
							if _, wErr := w.Write(
								jsonData,
							); wErr != nil {
								config.Logger.Log(
									ctx,
									config.LogLevel,
									fmt.Sprintf(
										"Failed to write "+
											"JSON response: %v",
										wErr,
									),
								)
							}
						}
					} else {
						str, ok := config.Response.(string)
						if ok {
							if _, err := w.Write(
								[]byte(str),
							); err != nil {
								config.Logger.Log(
									ctx,
									config.LogLevel,
									fmt.Sprintf(
										"Failed to write "+
											"error response: %v",
										err,
									),
								)
							}
						}
					}
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
