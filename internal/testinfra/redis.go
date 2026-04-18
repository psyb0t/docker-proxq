package testinfra

import (
	"context"
	"errors"
	"fmt"

	"github.com/hibiken/asynq"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var errUnexpectedRedisOptType = errors.New(
	"unexpected redis option type",
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
		return nil, fmt.Errorf(
			"start redis container: %w", err,
		)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		_ = container.Terminate(ctx)

		return nil, fmt.Errorf(
			"get redis connection string: %w", err,
		)
	}

	opt, err := asynq.ParseRedisURI(connStr)
	if err != nil {
		_ = container.Terminate(ctx)

		return nil, fmt.Errorf(
			"parse redis URI: %w", err,
		)
	}

	clientOpt, ok := opt.(asynq.RedisClientOpt)
	if !ok {
		_ = container.Terminate(ctx)

		return nil, fmt.Errorf(
			"redis option type %T: %w",
			opt, errUnexpectedRedisOptType,
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
