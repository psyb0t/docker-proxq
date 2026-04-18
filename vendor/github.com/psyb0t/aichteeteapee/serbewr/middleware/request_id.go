package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/psyb0t/aichteeteapee"
	"github.com/psyb0t/common-go/slogging"
)

func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(
			func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				reqID := r.Header.Get(
					aichteeteapee.HeaderNameXRequestID,
				)
				if reqID == "" {
					reqID = uuid.New().String()
				}

				w.Header().Set(
					aichteeteapee.HeaderNameXRequestID,
					reqID,
				)

				ctx := context.WithValue(
					r.Context(),
					aichteeteapee.ContextKeyRequestID,
					reqID,
				)

				logger := slogging.GetLogger(ctx).With(
					"requestId", reqID,
				)

				ctx = slogging.GetCtxWithLogger(
					ctx, logger,
				)

				next.ServeHTTP(w, r.WithContext(ctx))
			},
		)
	}
}
