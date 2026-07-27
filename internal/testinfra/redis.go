package testinfra

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/ctxerrors"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

type Redis struct {
	Container *tcredis.RedisContainer
	Addr      string
}

func SetupRedis(ctx context.Context) (*Redis, error) {
	container, err := tcredis.Run(
		ctx, "redis:7-alpine",
	)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "start redis container",
		)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		_ = container.Terminate(ctx)

		return nil, ctxerrors.Wrap(
			err, "get redis connection string",
		)
	}

	opt, err := asynq.ParseRedisURI(connStr)
	if err != nil {
		_ = container.Terminate(ctx)

		return nil, ctxerrors.Wrap(
			err, "parse redis URI",
		)
	}

	clientOpt, ok := opt.(asynq.RedisClientOpt)
	if !ok {
		_ = container.Terminate(ctx)

		return nil, ctxerrors.Wrapf(
			errUnexpectedRedisOptType, "redis option type %T", opt,
		)
	}

	return &Redis{
		Container: container,
		Addr:      clientOpt.Addr,
	}, nil
}

func (r *Redis) RedisOpt() asynq.RedisClientOpt {
	return asynq.RedisClientOpt{Addr: r.Addr}
}

func (r *Redis) Teardown(ctx context.Context) {
	if r.Container != nil {
		_ = r.Container.Terminate(ctx)
	}
}
