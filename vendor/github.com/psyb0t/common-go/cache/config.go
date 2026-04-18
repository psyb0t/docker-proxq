package cache

import (
	"time"

	"github.com/psyb0t/ctxerrors"
	"github.com/psyb0t/gonfiguration"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Mode       string        `env:"CACHE_MODE"        default:"none"`
	TTL        time.Duration `env:"CACHE_TTL"         default:"5m"`
	MaxEntries int           `env:"CACHE_MAX_ENTRIES"  default:"10000"`
	RedisAddr  string        `env:"CACHE_REDIS_ADDR"  default:"127.0.0.1:6379"`
	RedisPass  string        `env:"CACHE_REDIS_PASSWORD"`
	RedisDB    int           `env:"CACHE_REDIS_DB"    default:"0"`
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

func FromConfig(cfg Config) (Cache, func(), error) {
	switch cfg.Mode {
	case "memory":
		c := NewMemory(cfg.MaxEntries)

		return c, func() { _ = c.Close() }, nil
	case "redis":
		rc := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPass,
			DB:       cfg.RedisDB,
		})

		c := NewRedis(rc)

		return c, func() {
			_ = c.Close()
			_ = rc.Close()
		}, nil
	case "none", "":
		return nil, func() {}, nil
	default:
		return nil, func() {}, ctxerrors.Wrap(
			ErrInvalidCacheMode,
			"unknown cache mode: "+cfg.Mode,
		)
	}
}
