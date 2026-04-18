package tests

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheHit(t *testing.T) {
	tests := []struct {
		name      string
		cacheMode string
		path      string
	}{
		{
			name:      "memory cache hit",
			cacheMode: "memory",
			path:      "/mem-cached",
		},
		{
			name:      "redis cache hit",
			cacheMode: "redis",
			path:      "/redis-cached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t, tt.cacheMode, nil)
			defer e.cleanup()

			jobID1 := submitJob(
				t, e.proxqURL,
				http.MethodGet, tt.path,
				nil,
			)

			pollStatus(
				t, e.proxqURL, jobID1, "",
				30*time.Second,
			)

			jobID2 := submitJob(
				t, e.proxqURL,
				http.MethodGet, tt.path,
				nil,
			)

			pollStatus(
				t, e.proxqURL, jobID2, "",
				30*time.Second,
			)

			counts := getRequestCounts(
				t, e.countsURL,
			)
			assert.Equal(
				t, 1, counts[tt.path],
				"expected 1 upstream hit",
			)
		})
	}
}

func TestCacheSkips5xx(t *testing.T) {
	e := setup(t, "memory", nil)
	defer e.cleanup()

	jobID1 := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/status/500",
		nil,
	)

	pollStatus(
		t, e.proxqURL, jobID1, "", 30*time.Second,
	)

	jobID2 := submitJob(
		t, e.proxqURL,
		http.MethodGet, "/status/500",
		nil,
	)

	pollStatus(
		t, e.proxqURL, jobID2, "", 30*time.Second,
	)

	counts := getRequestCounts(t, e.countsURL)
	assert.Equal(
		t, 2, counts["/status/500"],
		"5xx should not be cached",
	)
}
