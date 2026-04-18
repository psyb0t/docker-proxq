package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobLifecycle(t *testing.T) {
	e := setup(t, setupOpts{})
	defer e.cleanup()

	tests := []struct {
		name       string
		url        string
		expectCode int
		hasSource  bool
	}{
		{
			name:       "status of nonexistent job",
			url:        "/__jobs/nonexistent-id",
			expectCode: http.StatusNotFound,
			hasSource:  true,
		},
		{
			name:       "content of nonexistent job",
			url:        "/__jobs/nonexistent-id/content",
			expectCode: http.StatusNotFound,
			hasSource:  true,
		},
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := client.Get(
				e.proxqURL + tt.url,
			)
			require.NoError(t, err)

			defer func() { _ = resp.Body.Close() }()

			assert.Equal(
				t, tt.expectCode, resp.StatusCode,
			)

			if tt.hasSource {
				assert.Equal(
					t, "proxq",
					resp.Header.Get("X-Proxq-Source"),
				)
			}
		})
	}
}

func TestCancelJob(t *testing.T) {
	e := setup(t, setupOpts{})
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/slow",
		nil,
	)

	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		e.proxqURL+"/__jobs/"+jobID,
		nil,
	)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	statusResp, err := client.Get(
		e.proxqURL + "/__jobs/" + jobID,
	)
	require.NoError(t, err)

	defer func() { _ = statusResp.Body.Close() }()

	assert.Equal(
		t, http.StatusNotFound, statusResp.StatusCode,
	)
}

func TestCustomJobsPath(t *testing.T) {
	e := setup(t, setupOpts{
		extraConfig: `jobsPath: "/status"`,
	})
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/hello",
		nil,
	)

	info := pollStatus(
		t, e.proxqURL, jobID, "/status",
		30*time.Second,
	)
	assert.Equal(t, "completed", info.Status)

	resp := getContent(
		t, e.proxqURL, jobID, "/status",
	)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestContentPending_HasProxqSourceHeader(
	t *testing.T,
) {
	e := setup(t, setupOpts{})
	defer e.cleanup()

	jobID := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/slow",
		nil,
	)

	resp := getContent(t, e.proxqURL, jobID, "")

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(
		t, http.StatusNotFound, resp.StatusCode,
	)
	assert.Equal(
		t, "proxq",
		resp.Header.Get("X-Proxq-Source"),
	)
}
