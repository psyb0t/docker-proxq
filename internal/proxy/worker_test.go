package proxy

import (
	"testing"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorker(t *testing.T) {
	tests := []struct {
		name          string
		cfg           WorkerConfig
		expectTimeout time.Duration
		expectTTL     time.Duration
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
			},
			expectTimeout: 10 * time.Second,
			expectTTL:     3 * time.Minute,
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
		})
	}
}

func TestBuildExcludeHeaders(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		expectLen int
		mustHave  []string
		mustNot   []string
	}{
		{
			name:      "nil uses defaults",
			input:     nil,
			expectLen: 4,
			mustHave: []string{
				aichteeteapee.HeaderNameXRequestID,
				aichteeteapee.HeaderNameXForwardedFor,
				aichteeteapee.HeaderNameXRealIP,
				aichteeteapee.HeaderNameXForwardedProto,
			},
		},
		{
			name:      "empty slice uses defaults",
			input:     []string{},
			expectLen: 4,
			mustHave: []string{
				aichteeteapee.HeaderNameXRequestID,
			},
		},
		{
			name: "custom replaces defaults",
			input: []string{
				"Authorization", "X-Trace-ID",
			},
			expectLen: 2,
			mustHave: []string{
				"Authorization", "X-Trace-ID",
			},
			mustNot: []string{
				aichteeteapee.HeaderNameXRequestID,
				aichteeteapee.HeaderNameXForwardedFor,
			},
		},
		{
			name:      "single header",
			input:     []string{"X-Custom"},
			expectLen: 1,
			mustHave:  []string{"X-Custom"},
			mustNot: []string{
				aichteeteapee.HeaderNameXRequestID,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildExcludeHeaders(tt.input)

			assert.Len(t, result, tt.expectLen)

			for _, h := range tt.mustHave {
				_, ok := result[h]
				assert.True(
					t, ok, "expected: %s", h,
				)
			}

			for _, h := range tt.mustNot {
				_, ok := result[h]
				assert.False(
					t, ok, "unexpected: %s", h,
				)
			}
		})
	}
}
