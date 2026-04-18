package proxy

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorker(t *testing.T) {
	customLogger := slog.Default()

	tests := []struct {
		name          string
		cfg           WorkerConfig
		expectTimeout time.Duration
		expectTTL     time.Duration
		customLogger  bool
	}{
		{
			name:          "all defaults",
			cfg:           WorkerConfig{},
			expectTimeout: defaultUpstreamTimeout,
		},
		{
			name: "custom values",
			cfg: WorkerConfig{
				UpstreamTimeout: 10 * time.Second,
				CacheTTL:        3 * time.Minute,
				Logger:          customLogger,
			},
			expectTimeout: 10 * time.Second,
			expectTTL:     3 * time.Minute,
			customLogger:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWorker(tt.cfg)

			require.NotNil(t, w)
			assert.NotNil(t, w.forwardCfg.HTTPClient)
			assert.Equal(
				t, tt.expectTimeout,
				w.forwardCfg.HTTPClient.Timeout,
			)
			assert.Equal(
				t, tt.expectTTL,
				w.forwardCfg.CacheTTL,
			)
			assert.NotNil(t, w.logger)

			if tt.customLogger {
				assert.Equal(t, customLogger, w.logger)
			}
		})
	}
}
