package proxy

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHandler(t *testing.T) {
	customLogger := slog.Default()

	tests := []struct {
		name              string
		cfg               HandlerConfig
		expectQueue       string
		expectRetention   time.Duration
		expectMaxBody     int64
		expectUpstreamURL string
		customLogger      bool
	}{
		{
			name: "all defaults",
			cfg: HandlerConfig{
				UpstreamURL: "http://upstream",
			},
			expectQueue:       DefaultQueue,
			expectRetention:   defaultTaskRetention,
			expectMaxBody:     defaultMaxRequestBodySize,
			expectUpstreamURL: "http://upstream",
		},
		{
			name: "custom values",
			cfg: HandlerConfig{
				UpstreamURL:        "http://custom:9090",
				MaxRequestBodySize: 1024,
				Queue:              "priority",
				TaskRetention:      30 * time.Minute,
				Logger:             customLogger,
			},
			expectQueue:       "priority",
			expectRetention:   30 * time.Minute,
			expectMaxBody:     1024,
			expectUpstreamURL: "http://custom:9090",
			customLogger:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(nil, tt.cfg)

			require.NotNil(t, h)
			assert.Equal(t, tt.expectUpstreamURL, h.upstreamURL)
			assert.Equal(t, tt.expectQueue, h.queue)
			assert.Equal(t, tt.expectRetention, h.taskRetention)
			assert.Equal(t, tt.expectMaxBody, h.maxRequestBodySize)
			assert.NotNil(t, h.logger)

			if tt.customLogger {
				assert.Equal(t, customLogger, h.logger)
			}
		})
	}
}
