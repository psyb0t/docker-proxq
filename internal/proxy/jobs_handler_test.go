package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	proxylib "github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJobsHandler(t *testing.T) {
	tests := []struct {
		name        string
		queue       string
		expectQueue string
	}{
		{
			name:        "default queue",
			queue:       "",
			expectQueue: DefaultQueue,
		},
		{
			name:        "custom queue",
			queue:       "custom",
			expectQueue: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewJobsHandler(nil, tt.queue)

			require.NotNil(t, h)
			assert.Equal(t, tt.expectQueue, h.queue)
		})
	}
}

func TestExtractJobID(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		path     string
		expected string
	}{
		{
			name:     "extracts id",
			pattern:  "GET /{id}",
			path:     "/abc-123",
			expected: "abc-123",
		},
		{
			name:     "uuid id",
			pattern:  "GET /{id}",
			path:     "/550e8400-e29b-41d4-a716-446655440000",
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "no path value returns empty",
			pattern:  "GET /other",
			path:     "/other",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotID string

			mux := http.NewServeMux()
			mux.HandleFunc(
				tt.pattern,
				func(
					_ http.ResponseWriter,
					r *http.Request,
				) {
					gotID = extractJobID(r)
				},
			)

			req := httptest.NewRequest(
				http.MethodGet, tt.path, nil,
			)
			mux.ServeHTTP(
				httptest.NewRecorder(), req,
			)

			assert.Equal(t, tt.expected, gotID)
		})
	}
}

func TestJobsHandler_EmptyID(t *testing.T) {
	h := NewJobsHandler(nil, "default")

	tests := []struct {
		name    string
		method  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{
			name:    "get returns bad request",
			method:  http.MethodGet,
			handler: h.Get,
		},
		{
			name:    "content returns bad request",
			method:  http.MethodGet,
			handler: h.Content,
		},
		{
			name:    "cancel returns bad request",
			method:  http.MethodDelete,
			handler: h.Cancel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				tt.method, "/__jobs/", nil,
			)
			rec := httptest.NewRecorder()

			tt.handler(rec, req)

			assert.Equal(
				t, http.StatusBadRequest, rec.Code,
			)
		})
	}
}

func TestSetProxqSourceHeader(t *testing.T) {
	rec := httptest.NewRecorder()

	setProxqSourceHeader(rec)

	assert.Equal(
		t, HeaderValueProxq,
		rec.Header().Get(HeaderNameXProxqSource),
	)
}

func TestWriteUpstreamResponse(t *testing.T) {
	tests := []struct {
		name          string
		result        *proxylib.ResponseResult
		expectCode    int
		expectBody    string
		expectHeaders map[string]string
	}{
		{
			name: "200 with body and headers",
			result: &proxylib.ResponseResult{
				StatusCode: http.StatusOK,
				Headers: map[string][]string{
					"Content-Type": {"application/json"},
					"X-Custom":     {"val"},
				},
				Body: []byte(`{"ok":true}`),
			},
			expectCode: http.StatusOK,
			expectBody: `{"ok":true}`,
			expectHeaders: map[string]string{
				"Content-Type": "application/json",
				"X-Custom":     "val",
			},
		},
		{
			name: "500 from upstream",
			result: &proxylib.ResponseResult{
				StatusCode: http.StatusInternalServerError,
				Body:       []byte("error"),
			},
			expectCode: http.StatusInternalServerError,
			expectBody: "error",
		},
		{
			name: "204 no body",
			result: &proxylib.ResponseResult{
				StatusCode: http.StatusNoContent,
			},
			expectCode: http.StatusNoContent,
			expectBody: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			writeUpstreamResponse(rec, tt.result)

			assert.Equal(t, tt.expectCode, rec.Code)
			assert.Equal(
				t, tt.expectBody, rec.Body.String(),
			)

			for k, v := range tt.expectHeaders {
				assert.Equal(t, v, rec.Header().Get(k))
			}
		})
	}
}
