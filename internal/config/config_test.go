package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	commonerrors "github.com/psyb0t/common-go/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func writeConfig(
	t *testing.T, content string,
) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(
		path, []byte(content), 0o600,
	))

	return path
}

func TestParse_Minimal(t *testing.T) {
	path := writeConfig(t, `
upstreams:
  - prefix: "/"
    url: "http://backend:8080"
`)

	cfg, err := Parse(path)
	require.NoError(t, err)

	assert.Equal(t, DefaultListenAddress, cfg.ListenAddress)
	assert.Equal(t, DefaultRedisAddr, cfg.Redis.Addr)
	assert.Equal(t, "", cfg.Redis.Password)
	assert.Equal(t, 0, cfg.Redis.DB)
	assert.Equal(t, DefaultQueue, cfg.Queue)
	assert.Equal(t, DefaultConcurrency, cfg.Concurrency)
	assert.Equal(t, DefaultJobsPath, cfg.JobsPath)
	assert.Equal(t, DefaultTaskRetention, cfg.TaskRetention)
	assert.Equal(t, CacheModeNone, cfg.Cache.Mode)
	assert.Equal(t, DefaultCacheTTL, cfg.Cache.TTL)
	assert.Equal(
		t, DefaultCacheMaxEntries, cfg.Cache.MaxEntries,
	)
	assert.Equal(
		t, DefaultCacheRedisKeyPrefix,
		cfg.Cache.RedisKeyPrefix,
	)

	require.Len(t, cfg.Upstreams, 1)

	u := cfg.Upstreams[0]
	assert.Equal(t, "/", u.Prefix)
	assert.Equal(t, "http://backend:8080", u.URL)
	assert.Equal(t, DefaultUpstreamTimeout, u.Timeout)
	assert.Equal(t, DefaultMaxBodySize, u.MaxBodySize)
	assert.Equal(
		t, DefaultDirectProxyThreshold,
		u.DirectProxyThreshold,
	)
	assert.Equal(
		t, DirectProxyModeProxy, u.DirectProxyMode,
	)
	assert.Equal(
		t, PathFilterModeBlacklist, u.PathFilter.Mode,
	)
	assert.Empty(t, u.CacheKeyExcludeHeaders)
	assert.Empty(t, u.PathFilter.Patterns)
	assert.Empty(t, u.CompiledPatterns)
}

func TestParse_FullConfig(t *testing.T) {
	path := writeConfig(t, `
listenAddress: "0.0.0.0:9090"
redis:
  addr: "redis:6380"
  password: "secret"
  db: 3
queue: "critical"
concurrency: 20
jobsPath: "/status"
taskRetention: "2h"
cache:
  mode: "memory"
  ttl: "10m"
  maxEntries: 5000
  redisKeyPrefix: "app:"
upstreams:
  - prefix: "/api"
    url: "http://api:3000"
    timeout: "10m"
    maxRetries: 3
    retryDelay: "5s"
    maxBodySize: 1024
    directProxyThreshold: 2048
    directProxyMode: "redirect"
    cacheKeyExcludeHeaders:
      - "Authorization"
      - "X-Trace-ID"
    pathFilter:
      mode: "whitelist"
      patterns:
        - "^/api/v[0-9]+"
  - prefix: "/web"
    url: "http://web:8080"
`)

	cfg, err := Parse(path)
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9090", cfg.ListenAddress)
	assert.Equal(t, "redis:6380", cfg.Redis.Addr)
	assert.Equal(t, "secret", cfg.Redis.Password)
	assert.Equal(t, 3, cfg.Redis.DB)
	assert.Equal(t, "critical", cfg.Queue)
	assert.Equal(t, 20, cfg.Concurrency)
	assert.Equal(t, "/status", cfg.JobsPath)
	assert.Equal(
		t, Duration(2*time.Hour), cfg.TaskRetention,
	)
	assert.Equal(t, CacheModeMemory, cfg.Cache.Mode)
	assert.Equal(
		t, Duration(10*time.Minute), cfg.Cache.TTL,
	)
	assert.Equal(t, 5000, cfg.Cache.MaxEntries)
	assert.Equal(t, "app:", cfg.Cache.RedisKeyPrefix)

	require.Len(t, cfg.Upstreams, 2)

	assert.Equal(t, "/api", cfg.Upstreams[0].Prefix)
	assert.Equal(t, "/web", cfg.Upstreams[1].Prefix)

	api := cfg.Upstreams[0]
	assert.Equal(t, "http://api:3000", api.URL)
	assert.Equal(
		t, Duration(10*time.Minute), api.Timeout,
	)
	assert.Equal(t, 3, api.MaxRetries)
	assert.Equal(
		t, Duration(5*time.Second), api.RetryDelay,
	)
	assert.Equal(t, int64(1024), api.MaxBodySize)
	assert.Equal(
		t, int64(2048), api.DirectProxyThreshold,
	)
	assert.Equal(
		t, DirectProxyModeRedirect, api.DirectProxyMode,
	)
	require.Len(t, api.CacheKeyExcludeHeaders, 2)
	assert.Equal(
		t, "Authorization",
		api.CacheKeyExcludeHeaders[0],
	)
	assert.Equal(
		t, "X-Trace-ID",
		api.CacheKeyExcludeHeaders[1],
	)
	assert.Equal(
		t, PathFilterModeWhitelist, api.PathFilter.Mode,
	)
	require.Len(t, api.CompiledPatterns, 1)
}

func TestParse_SortsByPrefixLength(t *testing.T) {
	path := writeConfig(t, `
upstreams:
  - prefix: "/short"
    url: "http://short:8080"
  - prefix: "/much-longer-prefix"
    url: "http://long:3000"
`)

	cfg, err := Parse(path)
	require.NoError(t, err)

	require.Len(t, cfg.Upstreams, 2)
	assert.Equal(
		t, "/much-longer-prefix",
		cfg.Upstreams[0].Prefix,
	)
	assert.Equal(t, "/short", cfg.Upstreams[1].Prefix)
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		fileMiss  bool
		expectErr error
	}{
		{
			name:      "file not found",
			fileMiss:  true,
			expectErr: os.ErrNotExist,
		},
		{
			name: "invalid yaml",
			yaml: "{{{{",
		},
		{
			name:      "no upstreams",
			yaml:      "queue: default",
			expectErr: ErrNoUpstreams,
		},
		{
			name: "missing prefix",
			yaml: `
upstreams:
  - url: "http://backend:8080"
`,
			expectErr: commonerrors.ErrRequiredFieldNotSet,
		},
		{
			name: "missing url",
			yaml: `
upstreams:
  - prefix: "/api"
`,
			expectErr: commonerrors.ErrRequiredFieldNotSet,
		},
		{
			name: "invalid regex pattern",
			yaml: `
upstreams:
  - prefix: "/"
    url: "http://backend:8080"
    pathFilter:
      patterns:
        - "[invalid"
`,
		},
		{
			name: "root with multiple upstreams",
			yaml: `
upstreams:
  - prefix: "/"
    url: "http://default:8080"
  - prefix: "/api"
    url: "http://api:3000"
`,
			expectErr: ErrRootWithMultipleUpstreams,
		},
		{
			name: "nested prefixes",
			yaml: `
upstreams:
  - prefix: "/api"
    url: "http://api:3000"
  - prefix: "/api/v2"
    url: "http://v2:3000"
`,
			expectErr: ErrNestedPrefixes,
		},
		{
			name: "prefix conflicts with jobs path",
			yaml: `
upstreams:
  - prefix: "/__jobs"
    url: "http://backend:8080"
`,
			expectErr: ErrPrefixConflictsWithJobsPath,
		},
		{
			name: "prefix is parent of jobs path",
			yaml: `
jobsPath: "/api/jobs"
upstreams:
  - prefix: "/api"
    url: "http://backend:8080"
`,
			expectErr: ErrPrefixConflictsWithJobsPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.fileMiss {
				path = "/nonexistent/config.yaml"
			} else {
				path = writeConfig(t, tt.yaml)
			}

			_, err := Parse(path)
			require.Error(t, err)

			if tt.expectErr != nil {
				assert.ErrorIs(t, err, tt.expectErr)
			}
		})
	}
}

func TestDuration_UnmarshalYAML_Invalid(t *testing.T) {
	var out struct {
		D Duration `yaml:"d"`
	}

	err := yaml.Unmarshal(
		[]byte(`d: "not-a-duration"`), &out,
	)
	require.Error(t, err)
}

func TestDuration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{
			name:     "minutes",
			input:    `d: "5m"`,
			expected: 5 * time.Minute,
		},
		{
			name:     "hours",
			input:    `d: "2h"`,
			expected: 2 * time.Hour,
		},
		{
			name:     "seconds",
			input:    `d: "30s"`,
			expected: 30 * time.Second,
		},
		{
			name:     "compound",
			input:    `d: "1h30m"`,
			expected: 90 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out struct {
				D Duration `yaml:"d"`
			}

			require.NoError(t, yaml.Unmarshal(
				[]byte(tt.input), &out,
			))
			assert.Equal(
				t, tt.expected, out.D.Std(),
			)
		})
	}
}
