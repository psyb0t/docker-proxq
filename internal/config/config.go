package config

import (
	"time"

	"github.com/psyb0t/ctxerrors"
	"github.com/psyb0t/gonfiguration"
)

// Config holds all runtime configuration for proxq.
type Config struct {
	UpstreamURL string `env:"PROXQ_UPSTREAM_URL"`

	RedisAddr     string `default:"127.0.0.1:6379"   env:"PROXQ_REDIS_ADDR"`
	RedisPassword string `env:"PROXQ_REDIS_PASSWORD"`
	RedisDB       int    `default:"0"                env:"PROXQ_REDIS_DB"`

	MaxBodySize int64 `default:"10485760" env:"PROXQ_MAX_REQUEST_BODY_SIZE"`

	UpstreamTimeout time.Duration `default:"5m" env:"PROXQ_UPSTREAM_TIMEOUT"`
	TaskRetention   time.Duration `default:"1h" env:"PROXQ_TASK_RETENTION"`

	Queue       string `default:"default" env:"PROXQ_QUEUE"`
	Concurrency int    `default:"10"      env:"PROXQ_CONCURRENCY"`
	JobsPath    string `default:"/__jobs" env:"PROXQ_JOBS_PATH"`
}

// Parse reads config from environment variables.
func Parse() (Config, error) {
	cfg := Config{}
	if err := gonfiguration.Parse(&cfg); err != nil {
		return Config{}, ctxerrors.Wrap(
			err, "parse config",
		)
	}

	return cfg, nil
}
