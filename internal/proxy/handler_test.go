package proxy

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/psyb0t/proxq/internal/config"
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
			name:     "no transfer encoding",
			expected: false,
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

func TestResolveUpstream(t *testing.T) {
	h := &Handler{
		upstreams: []upstream{
			{prefix: "/api/v2", url: "http://v2:3000"},
			{prefix: "/api", url: "http://api:3000"},
			{prefix: "/", url: "http://default:8080"},
		},
	}

	tests := []struct {
		name           string
		requestURI     string
		expectPrefix   string
		expectStripped string
	}{
		{
			name:           "exact prefix match",
			requestURI:     "/api",
			expectPrefix:   "/api",
			expectStripped: "/",
		},
		{
			name:           "prefix with subpath",
			requestURI:     "/api/users",
			expectPrefix:   "/api",
			expectStripped: "/users",
		},
		{
			name:           "longer prefix wins",
			requestURI:     "/api/v2/items",
			expectPrefix:   "/api/v2",
			expectStripped: "/items",
		},
		{
			name:           "query string preserved",
			requestURI:     "/api/users?page=1",
			expectPrefix:   "/api",
			expectStripped: "/users?page=1",
		},
		{
			name:           "root catch-all",
			requestURI:     "/other/path",
			expectPrefix:   "/",
			expectStripped: "/other/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet, tt.requestURI, nil,
			)

			u, stripped := h.resolveUpstream(req)

			require.NotNil(t, u)
			assert.Equal(t, tt.expectPrefix, u.prefix)
			assert.Equal(t, tt.expectStripped, stripped)
		})
	}
}

func TestResolveUpstream_NoMatch(t *testing.T) {
	h := &Handler{
		upstreams: []upstream{
			{prefix: "/api", url: "http://api:3000"},
		},
	}

	req := httptest.NewRequest(
		http.MethodGet, "/other", nil,
	)

	u, _ := h.resolveUpstream(req)
	assert.Nil(t, u)
}

func TestResolveUpstream_NoFalsePrefix(t *testing.T) {
	h := &Handler{
		upstreams: []upstream{
			{prefix: "/api", url: "http://api:3000"},
		},
	}

	req := httptest.NewRequest(
		http.MethodGet, "/api2/data", nil,
	)

	u, _ := h.resolveUpstream(req)
	assert.Nil(t, u)
}

func TestShouldBypassQueue(t *testing.T) {
	tests := []struct {
		name             string
		threshold        int64
		contentLength    int64
		transferEncoding []string
		filterPatterns   []*regexp.Regexp
		filterMode       string
		path             string
		expected         bool
	}{
		{
			name:          "below threshold",
			threshold:     1024,
			contentLength: 512,
			path:          "/data",
			expected:      false,
		},
		{
			name:          "above threshold",
			threshold:     1024,
			contentLength: 2048,
			path:          "/data",
			expected:      true,
		},
		{
			name:             "chunked always bypasses",
			transferEncoding: []string{"chunked"},
			path:             "/data",
			expected:         true,
		},
		{
			name:          "threshold zero disables",
			threshold:     0,
			contentLength: 999999,
			path:          "/data",
			expected:      false,
		},
		{
			name: "blacklist match bypasses",
			filterPatterns: []*regexp.Regexp{
				regexp.MustCompile(`^/uploads`),
			},
			filterMode: config.PathFilterModeBlacklist,
			path:       "/uploads/big.iso",
			expected:   true,
		},
		{
			name: "whitelist no match bypasses",
			filterPatterns: []*regexp.Regexp{
				regexp.MustCompile(`^/api`),
			},
			filterMode: config.PathFilterModeWhitelist,
			path:       "/uploads/big.iso",
			expected:   true,
		},
		{
			name: "whitelist match queues",
			filterPatterns: []*regexp.Regexp{
				regexp.MustCompile(`^/api`),
			},
			filterMode: config.PathFilterModeWhitelist,
			path:       "/api/data",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &upstream{
				directProxyThreshold: tt.threshold,
				pathFilter:           tt.filterPatterns,
				pathFilterMode:       tt.filterMode,
			}

			req := httptest.NewRequest(
				http.MethodPost, tt.path, nil,
			)
			req.ContentLength = tt.contentLength
			req.TransferEncoding = tt.transferEncoding

			assert.Equal(
				t, tt.expected,
				shouldBypassQueue(req, u),
			)
		})
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		expected string
	}{
		{
			name:     "root prefix",
			path:     "/hello",
			prefix:   "/",
			expected: "/hello",
		},
		{
			name:     "strip api",
			path:     "/api/users",
			prefix:   "/api",
			expected: "/users",
		},
		{
			name:     "exact match returns slash",
			path:     "/api",
			prefix:   "/api",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(
				t, tt.expected,
				stripPrefix(tt.path, tt.prefix),
			)
		})
	}
}

func TestNewHandler(t *testing.T) {
	tests := []struct {
		name            string
		cfg             HandlerConfig
		expectQueue     string
		expectRetention time.Duration
		expectUpstreams int
	}{
		{
			name: "defaults",
			cfg: HandlerConfig{
				Upstreams: []UpstreamConfig{
					{
						Prefix: "/",
						URL:    "http://backend:8080",
					},
				},
			},
			expectQueue:     DefaultQueue,
			expectRetention: defaultTaskRetention,
			expectUpstreams: 1,
		},
		{
			name: "custom values",
			cfg: HandlerConfig{
				Queue:         "priority",
				TaskRetention: 30 * time.Minute,
				Upstreams: []UpstreamConfig{
					{Prefix: "/a", URL: "http://a:80"},
					{Prefix: "/b", URL: "http://b:80"},
				},
			},
			expectQueue:     "priority",
			expectRetention: 30 * time.Minute,
			expectUpstreams: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(nil, tt.cfg)

			require.NotNil(t, h)
			assert.Equal(t, tt.expectQueue, h.queue)
			assert.Equal(
				t, tt.expectRetention, h.taskRetention,
			)
			assert.Len(
				t, h.upstreams, tt.expectUpstreams,
			)
		})
	}
}

func TestNewHandler_TrailingSlashStripped(
	t *testing.T,
) {
	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{
				Prefix: "/api",
				URL:    "http://backend:8080/v1/",
			},
		},
	})

	assert.Equal(
		t, "http://backend:8080/v1",
		h.upstreams[0].url,
	)
}
