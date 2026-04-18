package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Defaults(t *testing.T) {
	t.Setenv("PROXQ_UPSTREAM_URL", "")

	cfg, err := Parse()
	require.NoError(t, err)

	assert.Equal(t, "", cfg.UpstreamURL)
	assert.Equal(t, "127.0.0.1:6379", cfg.RedisAddr)
	assert.Equal(t, "", cfg.RedisPassword)
	assert.Equal(t, 0, cfg.RedisDB)
	assert.Equal(t, int64(10485760), cfg.MaxBodySize)
	assert.Equal(t, int64(10485760), cfg.DirectProxyThreshold)
	assert.Equal(t, "", cfg.DirectProxyPaths)
	assert.Equal(t, 5*time.Minute, cfg.UpstreamTimeout)
	assert.Equal(t, 1*time.Hour, cfg.TaskRetention)
	assert.Equal(t, "default", cfg.Queue)
	assert.Equal(t, 10, cfg.Concurrency)
	assert.Equal(t, "/__jobs", cfg.JobsPath)
	assert.Equal(t, "127.0.0.1:8080", cfg.ListenAddress)
}

func TestParse_InvalidValue(t *testing.T) {
	t.Setenv("PROXQ_REDIS_DB", "not-a-number")

	_, err := Parse()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse config")
}

func TestParse_EnvOverrides(t *testing.T) {
	t.Setenv("PROXQ_UPSTREAM_URL", "http://upstream:9090")
	t.Setenv("PROXQ_REDIS_ADDR", "redis:6380")
	t.Setenv("PROXQ_REDIS_PASSWORD", "secret")
	t.Setenv("PROXQ_REDIS_DB", "3")
	t.Setenv("PROXQ_MAX_REQUEST_BODY_SIZE", "1024")
	t.Setenv("PROXQ_DIRECT_PROXY_THRESHOLD", "2048")
	t.Setenv("PROXQ_DIRECT_PROXY_PATHS", "^/uploads,^/ws")
	t.Setenv("PROXQ_UPSTREAM_TIMEOUT", "10m")
	t.Setenv("PROXQ_TASK_RETENTION", "2h")
	t.Setenv("PROXQ_QUEUE", "critical")
	t.Setenv("PROXQ_CONCURRENCY", "20")
	t.Setenv("PROXQ_JOBS_PATH", "/status")
	t.Setenv("PROXQ_LISTENADDRESS", "0.0.0.0:9090")

	cfg, err := Parse()
	require.NoError(t, err)

	assert.Equal(t, "http://upstream:9090", cfg.UpstreamURL)
	assert.Equal(t, "redis:6380", cfg.RedisAddr)
	assert.Equal(t, "secret", cfg.RedisPassword)
	assert.Equal(t, 3, cfg.RedisDB)
	assert.Equal(t, int64(1024), cfg.MaxBodySize)
	assert.Equal(t, int64(2048), cfg.DirectProxyThreshold)
	assert.Equal(t, "^/uploads,^/ws", cfg.DirectProxyPaths)
	assert.Equal(t, 10*time.Minute, cfg.UpstreamTimeout)
	assert.Equal(t, 2*time.Hour, cfg.TaskRetention)
	assert.Equal(t, "critical", cfg.Queue)
	assert.Equal(t, 20, cfg.Concurrency)
	assert.Equal(t, "/status", cfg.JobsPath)
	assert.Equal(t, "0.0.0.0:9090", cfg.ListenAddress)
}
