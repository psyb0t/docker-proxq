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
	e := setup(t, "none", nil)
	defer e.cleanup()

	tests := []struct {
		name       string
		url        string
		expectCode int
	}{
		{
			name:       "status of nonexistent job",
			url:        "/__jobs/nonexistent-id",
			expectCode: http.StatusNotFound,
		},
		{
			name:       "content of nonexistent job",
			url:        "/__jobs/nonexistent-id/content",
			expectCode: http.StatusNotFound,
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
		})
	}
}

func TestCancelJob(t *testing.T) {
	e := setup(t, "none", nil)
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
	e := setup(t, "none", map[string]string{
		"PROXQ_JOBS_PATH": "/status",
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
