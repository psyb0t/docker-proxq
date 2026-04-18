package slogging

import (
	"context"
	"log/slog"
)

type contextKey struct{}

func GetCtxWithLogger(
	ctx context.Context,
	logger *slog.Logger,
) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	return context.WithValue(ctx, contextKey{}, logger)
}

func GetLogger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(
		contextKey{},
	).(*slog.Logger); ok {
		return logger
	}

	return slog.Default()
}
