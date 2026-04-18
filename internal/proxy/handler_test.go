package proxy

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name       string
		connection string
		upgrade    string
		expected   bool
	}{
		{
			name:       "valid websocket upgrade",
			connection: "upgrade",
			upgrade:    "websocket",
			expected:   true,
		},
		{
			name:       "case insensitive",
			connection: "Upgrade",
			upgrade:    "WebSocket",
			expected:   true,
		},
		{
			name:       "missing connection header",
			connection: "",
			upgrade:    "websocket",
			expected:   false,
		},
		{
			name:       "missing upgrade header",
			connection: "upgrade",
			upgrade:    "",
			expected:   false,
		},
		{
			name:       "wrong connection value",
			connection: "keep-alive",
			upgrade:    "websocket",
			expected:   false,
		},
		{
			name:       "wrong upgrade value",
			connection: "upgrade",
			upgrade:    "h2c",
			expected:   false,
		},
		{
			name:     "no headers",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet, "/ws", nil,
			)

			if tt.connection != "" {
				req.Header.Set(
					aichteeteapee.HeaderNameConnection,
					tt.connection,
				)
			}

			if tt.upgrade != "" {
				req.Header.Set(
					aichteeteapee.HeaderNameUpgrade,
					tt.upgrade,
				)
			}

			assert.Equal(
				t, tt.expected,
				isWebSocketUpgrade(req),
			)
		})
	}
}

func TestIsChunkedTransfer(t *testing.T) {
	tests := []struct {
		name             string
		transferEncoding []string
		expected         bool
	}{
		{
			name:             "chunked",
			transferEncoding: []string{"chunked"},
			expected:         true,
		},
		{
			name:             "Chunked uppercase",
			transferEncoding: []string{"Chunked"},
			expected:         true,
		},
		{
			name:             "no transfer encoding",
			transferEncoding: nil,
			expected:         false,
		},
		{
			name:             "identity",
			transferEncoding: []string{"identity"},
			expected:         false,
		},
		{
			name:             "multiple with chunked",
			transferEncoding: []string{"gzip", "chunked"},
			expected:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost, "/data", nil,
			)
			req.TransferEncoding = tt.transferEncoding

			assert.Equal(
				t, tt.expected,
				isChunkedTransfer(req),
			)
		})
	}
}

func TestShouldDirectProxy(t *testing.T) {
	tests := []struct {
		name             string
		threshold        int64
		contentLength    int64
		transferEncoding []string
		expected         bool
	}{
		{
			name:          "below threshold",
			threshold:     1024,
			contentLength: 512,
			expected:      false,
		},
		{
			name:          "above threshold",
			threshold:     1024,
			contentLength: 2048,
			expected:      true,
		},
		{
			name:          "equal to threshold",
			threshold:     1024,
			contentLength: 1024,
			expected:      false,
		},
		{
			name:             "chunked always direct",
			threshold:        0,
			contentLength:    -1,
			transferEncoding: []string{"chunked"},
			expected:         true,
		},
		{
			name:          "threshold zero disables",
			threshold:     0,
			contentLength: 999999,
			expected:      false,
		},
		{
			name:          "unknown length not chunked",
			threshold:     1024,
			contentLength: -1,
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				directProxyThreshold: tt.threshold,
			}

			req := httptest.NewRequest(
				http.MethodPost, "/upload", nil,
			)
			req.ContentLength = tt.contentLength
			req.TransferEncoding = tt.transferEncoding

			assert.Equal(
				t, tt.expected,
				h.shouldDirectProxy(req),
			)
		})
	}
}

func TestMatchesDirectProxyPath(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		expected bool
	}{
		{
			name:     "matches upload path",
			patterns: []string{`^/uploads`},
			path:     "/uploads/image.png",
			expected: true,
		},
		{
			name:     "matches ws path",
			patterns: []string{`^/ws`},
			path:     "/ws/connect",
			expected: true,
		},
		{
			name:     "no match",
			patterns: []string{`^/uploads`, `^/ws`},
			path:     "/api/data",
			expected: false,
		},
		{
			name:     "empty patterns",
			patterns: nil,
			path:     "/anything",
			expected: false,
		},
		{
			name:     "exact match",
			patterns: []string{`^/stream$`},
			path:     "/stream",
			expected: true,
		},
		{
			name:     "exact no match on subpath",
			patterns: []string{`^/stream$`},
			path:     "/stream/data",
			expected: false,
		},
		{
			name:     "matches second pattern",
			patterns: []string{`^/nope`, `^/yes`},
			path:     "/yes/please",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var compiled []*regexp.Regexp

			for _, p := range tt.patterns {
				compiled = append(
					compiled, regexp.MustCompile(p),
				)
			}

			h := &Handler{
				directProxyPaths: compiled,
			}

			assert.Equal(
				t, tt.expected,
				h.matchesDirectProxyPath(tt.path),
			)
		})
	}
}

func TestShouldDirectProxy_PathMatch(t *testing.T) {
	h := &Handler{
		directProxyPaths: []*regexp.Regexp{
			regexp.MustCompile(`^/uploads`),
		},
	}

	req := httptest.NewRequest(
		http.MethodPost, "/uploads/big.iso", nil,
	)

	assert.True(t, h.shouldDirectProxy(req))
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name              string
		cfg               HandlerConfig
		expectQueue       string
		expectRetention   time.Duration
		expectMaxBody     int64
		expectUpstreamURL string
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
			},
			expectQueue:       "priority",
			expectRetention:   30 * time.Minute,
			expectMaxBody:     1024,
			expectUpstreamURL: "http://custom:9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(nil, tt.cfg)

			require.NotNil(t, h)
			assert.Equal(
				t, tt.expectUpstreamURL,
				h.upstreamURL,
			)
			assert.Equal(
				t, tt.expectQueue, h.queue,
			)
			assert.Equal(
				t, tt.expectRetention,
				h.taskRetention,
			)
			assert.Equal(
				t, tt.expectMaxBody,
				h.maxRequestBodySize,
			)
			assert.NotNil(t, h.reverseProxy)
		})
	}
}
