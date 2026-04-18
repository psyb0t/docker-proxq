package cache

import (
	"context"
	"errors"
	"time"

	"github.com/psyb0t/ctxerrors"
	"github.com/redis/go-redis/v9"
)

const redisKeyPrefix = "cache:"

type Redis struct {
	client    redis.UniversalClient
	keyPrefix string
}

func NewRedis(client redis.UniversalClient) *Redis {
	return &Redis{client: client}
}

func NewRedisWithPrefix(
	client redis.UniversalClient,
	prefix string,
) *Redis {
	return &Redis{
		client:    client,
		keyPrefix: prefix,
	}
}

func (r *Redis) buildKey(key string) string {
	return r.keyPrefix + redisKeyPrefix + key
}

func (r *Redis) Get(
	ctx context.Context,
	key string,
) ([]byte, error) {
	val, err := r.client.Get(ctx, r.buildKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}

	if err != nil {
		return nil, ctxerrors.Wrap(err, "redis get")
	}

	return val, nil
}

func (r *Redis) Set(
	ctx context.Context,
	key string,
	val []byte,
	ttl time.Duration,
) error {
	err := r.client.Set(
		ctx, r.buildKey(key), val, ttl,
	).Err()
	if err != nil {
		return ctxerrors.Wrap(err, "redis set")
	}

	return nil
}

func (r *Redis) Delete(
	ctx context.Context,
	key string,
) error {
	err := r.client.Del(ctx, r.buildKey(key)).Err()
	if err != nil {
		return ctxerrors.Wrap(err, "redis delete")
	}

	return nil
}

func (r *Redis) Close() error {
	return nil
}
