package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
		path     string
		expected string
	}{
		{
			name:     "id from path",
			path:     "/__jobs/abc-123",
			expected: "abc-123",
		},
		{
			name:     "id with trailing slash",
			path:     "/__jobs/xyz-456/",
			expected: "xyz-456",
		},
		{
			name:     "empty id",
			path:     "/__jobs/",
			expected: "",
		},
		{
			name:     "non-jobs path",
			path:     "/other/path",
			expected: "/other/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet, tt.path, nil,
			)
			assert.Equal(
				t, tt.expected, extractJobID(req),
			)
		})
	}
}

func TestExtractJobID_PathValue(t *testing.T) {
	mux := http.NewServeMux()

	var gotID string

	mux.HandleFunc(
		"GET /__jobs/{id}",
		func(_ http.ResponseWriter, r *http.Request) {
			gotID = extractJobID(r)
		},
	)

	req := httptest.NewRequest(
		http.MethodGet, "/__jobs/uuid-here", nil,
	)

	mux.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "uuid-here", gotID)
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
