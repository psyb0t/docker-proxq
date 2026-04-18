package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectProxyBypass(t *testing.T) {
	tests := []struct {
		name     string
		extraEnv map[string]string
		method   string
		path     string
		body     string
	}{
		{
			name: "large body exceeds threshold",
			extraEnv: map[string]string{
				"PROXQ_DIRECT_PROXY_THRESHOLD": "100",
			},
			method: http.MethodPost,
			path:   "/echo",
			body:   strings.Repeat("x", 200),
		},
		{
			name: "path regex match",
			extraEnv: map[string]string{
				"PROXQ_DIRECT_PROXY_PATHS": "^/direct",
			},
			method: http.MethodGet,
			path:   "/direct/something",
		},
		{
			name: "multiple path patterns",
			extraEnv: map[string]string{
				"PROXQ_DIRECT_PROXY_PATHS": "^/uploads,^/stream",
			},
			method: http.MethodGet,
			path:   "/stream/data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t, "none", tt.extraEnv)
			defer e.cleanup()

			client := &http.Client{
				Timeout: 10 * time.Second,
			}

			var bodyReader io.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			}

			req, err := http.NewRequestWithContext(
				context.Background(),
				tt.method,
				e.proxqURL+tt.path,
				bodyReader,
			)
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)

			defer func() { _ = resp.Body.Close() }()

			assert.Equal(
				t, http.StatusOK, resp.StatusCode,
				"direct proxy should return upstream response",
			)

			var echo upstreamEcho

			require.NoError(t, json.NewDecoder(
				resp.Body,
			).Decode(&echo))
			assert.Equal(t, tt.method, echo.Method)
		})
	}
}

func TestQueuedNotBypassed(t *testing.T) {
	e := setup(t, "none", map[string]string{
		"PROXQ_DIRECT_PROXY_PATHS":     "^/direct",
		"PROXQ_DIRECT_PROXY_THRESHOLD": "1000000",
	})
	defer e.cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(
		e.proxqURL + "/not-matching",
	)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(
		t, http.StatusAccepted, resp.StatusCode,
		"non-matching path should be queued",
	)
}
