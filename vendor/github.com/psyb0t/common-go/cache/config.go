package cache

import (
	"time"

	"github.com/psyb0t/ctxerrors"
	"github.com/psyb0t/gonfiguration"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Mode           string        `default:"none"               env:"CACHE_MODE"`
	TTL            time.Duration `default:"5m"                 env:"CACHE_TTL"`
	MaxEntries     int           `default:"10000"              env:"CACHE_MAX_ENTRIES"`
	RedisKeyPrefix string        `env:"CACHE_REDIS_KEY_PREFIX"`
}

type Options struct {
	Config
	RedisClient redis.UniversalClient
}

func ParseConfig() (Config, error) {
	cfg := Config{}
	if err := gonfiguration.Parse(&cfg); err != nil {
		return Config{}, ctxerrors.Wrap(
			err, "parse cache config",
		)
	}

	return cfg, nil
}

func New(opts Options) (Cache, func(), error) { //nolint:ireturn
	switch opts.Mode {
	case "memory":
		c := NewMemory(opts.MaxEntries)

		return c, func() { _ = c.Close() }, nil
	case "redis":
		if opts.RedisClient == nil {
			return nil, func() {}, ctxerrors.Wrap(
				ErrInvalidCacheMode,
				"redis client required for redis cache mode",
			)
		}

		c := NewRedisWithPrefix(
			opts.RedisClient, opts.RedisKeyPrefix,
		)

		return c, func() { _ = c.Close() }, nil
	case "none", "":
		return nil, func() {}, nil
	default:
		return nil, func() {}, ctxerrors.Wrap(
			ErrInvalidCacheMode,
			"unknown cache mode: "+opts.Mode,
		)
	}
}
