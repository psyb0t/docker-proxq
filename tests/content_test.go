package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentTypes(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		expectCode  int
		contentType string
		checkBody   func(t *testing.T, body []byte)
	}{
		{
			name:        "JSON response",
			method:      http.MethodGet,
			path:        "/hello",
			expectCode:  http.StatusOK,
			contentType: "application/json",
			checkBody: func(t *testing.T, body []byte) {
				t.Helper()

				var echo upstreamEcho

				require.NoError(
					t, json.Unmarshal(body, &echo),
				)
				assert.Equal(
					t, http.MethodGet, echo.Method,
				)
			},
		},
		{
			name:        "plain text response",
			method:      http.MethodGet,
			path:        "/text",
			expectCode:  http.StatusOK,
			contentType: "text/plain",
			checkBody: func(t *testing.T, body []byte) {
				t.Helper()

				assert.Equal(
					t, "plain text response",
					string(body),
				)
			},
		},
		{
			name:        "PNG image response",
			method:      http.MethodGet,
			path:        "/image",
			expectCode:  http.StatusOK,
			contentType: "image/png",
			checkBody: func(t *testing.T, body []byte) {
				t.Helper()

				assert.True(
					t,
					bytes.HasPrefix(
						body,
						[]byte{0x89, 0x50, 0x4E, 0x47},
					),
					"expected PNG magic bytes",
				)
			},
		},
		{
			name:        "POST with JSON body",
			method:      http.MethodPost,
			path:        "/echo",
			body:        `{"test":"data"}`,
			expectCode:  http.StatusOK,
			contentType: "application/json",
			checkBody: func(t *testing.T, body []byte) {
				t.Helper()

				var echo upstreamEcho

				require.NoError(
					t, json.Unmarshal(body, &echo),
				)
				assert.Equal(
					t, http.MethodPost, echo.Method,
				)
				assert.Contains(
					t, echo.Body, `{"test":"data"}`,
				)
			},
		},
		{
			name:        "upstream 503 is completed",
			method:      http.MethodGet,
			path:        "/status/503",
			expectCode:  http.StatusServiceUnavailable,
			contentType: "application/json",
		},
		{
			name:        "upstream 404 is completed",
			method:      http.MethodGet,
			path:        "/status/404",
			expectCode:  http.StatusNotFound,
			contentType: "application/json",
		},
		{
			name:        "upstream 500 is completed",
			method:      http.MethodGet,
			path:        "/status/500",
			expectCode:  http.StatusInternalServerError,
			contentType: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader io.Reader
			if tt.body != "" {
				bodyReader = strings.NewReader(tt.body)
			}

			jobID := submitJob(
				t, e.proxqURL,
				tt.method, tt.path,
				bodyReader,
			)

			info := pollStatus(
				t, e.proxqURL, jobID, "",
				30*time.Second,
			)
			assert.Equal(t, "completed", info.Status)

			resp := getContent(
				t, e.proxqURL, jobID, "",
			)

			defer func() { _ = resp.Body.Close() }()

			assert.Equal(
				t, tt.expectCode, resp.StatusCode,
			)
			assert.Equal(
				t, tt.contentType,
				resp.Header.Get("Content-Type"),
			)

			if tt.checkBody != nil {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)

				tt.checkBody(t, body)
			}
		})
	}
}

func TestHeadersForwarded(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/headers",
		nil,
	)

	pollStatus(
		t, e.proxqURL, jobID, "", 30*time.Second,
	)

	resp := getContent(t, e.proxqURL, jobID, "")

	defer func() { _ = resp.Body.Close() }()

	var echo upstreamEcho

	require.NoError(t, json.NewDecoder(
		resp.Body,
	).Decode(&echo))

	key := strings.ToLower(
		aichteeteapee.HeaderNameXForwardedProto,
	)
	assert.NotEmpty(
		t, echo.Headers[key],
		"X-Forwarded-Proto missing",
	)
}

func TestTextResponseCustomHeaders(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/text",
		nil,
	)

	pollStatus(
		t, e.proxqURL, jobID, "", 30*time.Second,
	)

	resp := getContent(t, e.proxqURL, jobID, "")

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(
		t, "hello", resp.Header.Get("X-Custom"),
	)
}
