package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathTraversal(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	tests := []struct {
		name string
		path string
	}{
		{
			name: "dot dot slash",
			path: "/../../../etc/passwd",
		},
		{
			name: "encoded traversal",
			path: "/%2e%2e/%2e%2e/etc/passwd",
		},
		{
			name: "double encoded",
			path: "/%252e%252e/etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobID := submitJob(
				t, e.proxqURL,
				http.MethodGet, tt.path,
				nil,
			)

			info := pollStatus(
				t, e.proxqURL, jobID, "",
				30*time.Second,
			)

			assert.Equal(t, "completed", info.Status)
		})
	}
}

func TestLargeHeaders(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		e.proxqURL+"/echo",
		nil,
	)
	require.NoError(t, err)

	req.Header.Set(
		"X-Large", strings.Repeat("A", 8192),
	)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.True(
		t,
		resp.StatusCode == http.StatusAccepted ||
			resp.StatusCode == http.StatusRequestHeaderFieldsTooLarge,
	)
}

func TestBodyAtMaxSize(t *testing.T) {
	e := setup(t, "none", map[string]string{
		"PROXQ_MAX_REQUEST_BODY_SIZE":  "1024",
		"PROXQ_DIRECT_PROXY_THRESHOLD": "0",
	})
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodPost, "/echo",
		strings.NewReader(strings.Repeat("x", 1024)),
	)

	info := pollStatus(
		t, e.proxqURL, jobID, "", 30*time.Second,
	)
	assert.Equal(t, "completed", info.Status)

	resp := getContent(t, e.proxqURL, jobID, "")

	defer func() { _ = resp.Body.Close() }()

	var echo upstreamEcho

	require.NoError(t, json.NewDecoder(
		resp.Body,
	).Decode(&echo))
	assert.Len(t, echo.Body, 1024)
}

func TestBodyExceedsMaxSizeTruncated(t *testing.T) {
	e := setup(t, "none", map[string]string{
		"PROXQ_MAX_REQUEST_BODY_SIZE":  "100",
		"PROXQ_DIRECT_PROXY_THRESHOLD": "0",
	})
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodPost, "/echo",
		strings.NewReader(strings.Repeat("x", 500)),
	)

	info := pollStatus(
		t, e.proxqURL, jobID, "", 30*time.Second,
	)
	assert.Equal(t, "completed", info.Status)

	resp := getContent(t, e.proxqURL, jobID, "")

	defer func() { _ = resp.Body.Close() }()

	var echo upstreamEcho

	require.NoError(t, json.NewDecoder(
		resp.Body,
	).Decode(&echo))
	assert.LessOrEqual(t, len(echo.Body), 100)
}

func TestSpecialCharactersInPath(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	tests := []struct {
		name string
		path string
	}{
		{
			name: "spaces encoded",
			path: "/path%20with%20spaces",
		},
		{
			name: "unicode",
			path: "/api/données",
		},
		{
			name: "query string",
			path: "/search?q=test&page=1",
		},
		{
			name: "fragment ignored",
			path: "/page#section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobID := submitJob(
				t, e.proxqURL,
				http.MethodGet, tt.path,
				nil,
			)

			info := pollStatus(
				t, e.proxqURL, jobID, "",
				30*time.Second,
			)
			assert.Equal(t, "completed", info.Status)
		})
	}
}

func TestEmptyBody(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	tests := []struct {
		name   string
		method string
	}{
		{name: "GET no body", method: http.MethodGet},
		{name: "POST empty body", method: http.MethodPost},
		{name: "PUT empty body", method: http.MethodPut},
		{name: "DELETE no body", method: http.MethodDelete},
		{name: "PATCH empty body", method: http.MethodPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobID := submitJob(
				t, e.proxqURL,
				tt.method, "/echo",
				nil,
			)

			info := pollStatus(
				t, e.proxqURL, jobID, "",
				30*time.Second,
			)
			assert.Equal(t, "completed", info.Status)
		})
	}
}

func TestConcurrentRequests(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	const concurrency = 50

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		jobIDs []string
	)

	for i := range concurrency {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			client := &http.Client{
				Timeout: 10 * time.Second,
			}

			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				e.proxqURL+"/echo",
				nil,
			)
			if err != nil {
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				return
			}

			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusAccepted {
				return
			}

			var jr jobResponse

			if err := json.NewDecoder(
				resp.Body,
			).Decode(&jr); err != nil {
				return
			}

			mu.Lock()
			jobIDs = append(jobIDs, jr.JobID)
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	assert.Equal(
		t, concurrency, len(jobIDs),
		"all requests should be accepted",
	)

	completed := 0

	for _, id := range jobIDs {
		info := pollStatus(
			t, e.proxqURL, id, "", 60*time.Second,
		)
		if info.Status == "completed" {
			completed++
		}
	}

	assert.Equal(
		t, concurrency, completed,
		"all jobs should complete",
	)
}

func TestContentBeforeCompletion(t *testing.T) {
	e := setup(t, "none", nil)
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/slow",
		nil,
	)

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(
		e.proxqURL + "/__jobs/" + jobID + "/content",
	)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(
		t, http.StatusNotFound, resp.StatusCode,
		"content should 404 while job is pending",
	)
}

func TestMultipleContentFetches(t *testing.T) {
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

	for range 3 {
		resp := getContent(t, e.proxqURL, jobID, "")

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		require.NoError(t, err)
		assert.Equal(
			t, "plain text response", string(body),
		)
		assert.Equal(
			t, http.StatusOK, resp.StatusCode,
		)
	}
}
