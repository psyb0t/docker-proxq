package config

import (
	"time"

	"github.com/psyb0t/ctxerrors"
	"github.com/psyb0t/gonfiguration"
)

type Config struct {
	UpstreamURL string `env:"PROXQ_UPSTREAM_URL"`

	RedisAddr     string `env:"PROXQ_REDIS_ADDR"     default:"127.0.0.1:6379"`
	RedisPassword string `env:"PROXQ_REDIS_PASSWORD"`
	RedisDB       int    `env:"PROXQ_REDIS_DB"       default:"0"`

	MaxBodySize          int64  `env:"PROXQ_MAX_REQUEST_BODY_SIZE"  default:"10485760"` //nolint:lll
	DirectProxyThreshold int64  `env:"PROXQ_DIRECT_PROXY_THRESHOLD" default:"10485760"` //nolint:lll
	DirectProxyPaths     string `env:"PROXQ_DIRECT_PROXY_PATHS"`
	DirectProxyMode      string `env:"PROXQ_DIRECT_PROXY_MODE"      default:"proxy"` //nolint:lll

	UpstreamTimeout time.Duration `env:"PROXQ_UPSTREAM_TIMEOUT" default:"5m"`
	TaskRetention   time.Duration `env:"PROXQ_TASK_RETENTION"   default:"1h"`

	Queue         string `env:"PROXQ_QUEUE"         default:"default"`
	Concurrency   int    `env:"PROXQ_CONCURRENCY"   default:"10"`
	JobsPath      string `env:"PROXQ_JOBS_PATH"     default:"/__jobs"`
	ListenAddress string `env:"PROXQ_LISTENADDRESS" default:"127.0.0.1:8080"` //nolint:lll
}

func Parse() (Config, error) {
	cfg := Config{}
	if err := gonfiguration.Parse(&cfg); err != nil {
		return Config{}, ctxerrors.Wrap(
			err, "parse config",
		)
	}

	return cfg, nil
}
