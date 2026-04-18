package config

import (
	"time"

	"github.com/psyb0t/ctxerrors"
	"github.com/psyb0t/gonfiguration"
)

// Config holds all runtime configuration for proxq.
type Config struct {
	UpstreamURL        string        `env:"PROXQ_UPSTREAM_URL"`
	RedisAddr          string        `env:"PROXQ_REDIS_ADDR"            default:"127.0.0.1:6379"`
	RedisPassword      string        `env:"PROXQ_REDIS_PASSWORD"`
	RedisDB            int           `env:"PROXQ_REDIS_DB"              default:"0"`
	MaxRequestBodySize int64         `env:"PROXQ_MAX_REQUEST_BODY_SIZE" default:"10485760"`
	UpstreamTimeout    time.Duration `env:"PROXQ_UPSTREAM_TIMEOUT"      default:"5m"`
	TaskRetention      time.Duration `env:"PROXQ_TASK_RETENTION"        default:"1h"`
	Queue              string        `env:"PROXQ_QUEUE"                 default:"default"`
	Concurrency        int           `env:"PROXQ_CONCURRENCY"           default:"10"`
	JobsPath           string        `env:"PROXQ_JOBS_PATH"             default:"/__jobs"`
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
