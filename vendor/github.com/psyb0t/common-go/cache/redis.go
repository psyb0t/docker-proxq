package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client redis.UniversalClient
}

func NewRedis(client redis.UniversalClient) *Redis {
	return &Redis{client: client}
}

func (r *Redis) Get(
	ctx context.Context,
	key string,
) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}

	if err != nil {
		return nil, err
	}

	return val, nil
}

func (r *Redis) Set(
	ctx context.Context,
	key string,
	val []byte,
	ttl time.Duration,
) error {
	return r.client.Set(ctx, key, val, ttl).Err()
}

func (r *Redis) Delete(
	ctx context.Context,
	key string,
) error {
	return r.client.Del(ctx, key).Err()
}

func (r *Redis) Close() error {
	return nil
}
