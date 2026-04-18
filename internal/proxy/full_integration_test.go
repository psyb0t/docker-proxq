package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/proxq/internal/config"
	"github.com/psyb0t/proxq/internal/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fullEnv struct {
	handler   *Handler
	jobs      *JobsHandler
	inspector *asynq.Inspector
	queue     string
	cleanup   func()
}

func setupFull(
	t *testing.T,
	upstreamURL string,
	upstreams []UpstreamConfig,
) fullEnv {
	t.Helper()

	ctx := context.Background()

	rds, err := testinfra.SetupRedis(ctx)
	require.NoError(t, err)

	queue := "full-test"

	client := asynq.NewClient(rds.RedisOpt())
	inspector := asynq.NewInspector(rds.RedisOpt())

	if upstreams == nil {
		upstreams = []UpstreamConfig{
			{
				Prefix:      "/",
				URL:         upstreamURL,
				MaxBodySize: config.DefaultMaxBodySize,
			},
		}
	}

	handler := NewHandler(client, HandlerConfig{
		Upstreams:     upstreams,
		Queue:         queue,
		TaskRetention: 10 * time.Minute,
	})

	worker := NewWorker(WorkerConfig{})

	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskTypeName, worker.ProcessTask)

	srv := asynq.NewServer(
		rds.RedisOpt(),
		asynq.Config{
			Concurrency:    2,
			Queues:         map[string]int{queue: 1},
			RetryDelayFunc: RetryDelayFunc,
		},
	)

	go func() { _ = srv.Run(mux) }()

	jobs := NewJobsHandler(inspector, queue)

	return fullEnv{
		handler:   handler,
		jobs:      jobs,
		inspector: inspector,
		queue:     queue,
		cleanup: func() {
			srv.Shutdown()

			_ = client.Close()
			_ = inspector.Close()

			rds.Teardown(ctx)
		},
	}
}

func submitAndWait(
	t *testing.T,
	fe fullEnv,
	method, path string,
	body io.Reader,
) string {
	t.Helper()

	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()

	fe.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp acceptedResponse

	require.NoError(t, json.Unmarshal(
		rec.Body.Bytes(), &resp,
	))

	require.Eventually(t, func() bool {
		info, err := fe.inspector.GetTaskInfo(
			fe.queue, resp.JobID,
		)
		if err != nil {
			return false
		}

		return info.State == asynq.TaskStateCompleted
	}, 10*time.Second, 100*time.Millisecond)

	return resp.JobID
}

func getJobContent(
	t *testing.T,
	fe fullEnv,
	jobID string,
) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /__jobs/{id}/content",
		fe.jobs.Content,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/__jobs/"+jobID+"/content",
		nil,
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	return rec
}

func getJobStatus(
	t *testing.T,
	fe fullEnv,
	jobID string,
) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /__jobs/{id}",
		fe.jobs.Get,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/__jobs/"+jobID,
		nil,
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	return rec
}

func TestFull_JSONRoundTrip(t *testing.T) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"Content-Type", "application/json",
			)
			w.Header().Set("X-Custom", "test-value")
			w.WriteHeader(http.StatusOK)

			resp, _ := json.Marshal(map[string]string{
				"method": r.Method,
				"path":   r.URL.Path,
			})
			_, _ = w.Write(resp)
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	jobID := submitAndWait(
		t, fe, http.MethodPost, "/api/data", nil,
	)

	rec := getJobContent(t, fe, jobID)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(
		t, "application/json",
		rec.Header().Get("Content-Type"),
	)
	assert.Equal(
		t, "test-value",
		rec.Header().Get("X-Custom"),
	)

	var body map[string]string

	require.NoError(t, json.Unmarshal(
		rec.Body.Bytes(), &body,
	))
	assert.Equal(t, http.MethodPost, body["method"])
	assert.Equal(t, "/api/data", body["path"])

	assert.Empty(
		t, rec.Header().Get(HeaderNameXProxqSource),
	)
}

func TestFull_TextRoundTrip(t *testing.T) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.Header().Set(
				"Content-Type", "text/plain",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello world"))
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	jobID := submitAndWait(
		t, fe, http.MethodGet, "/text", nil,
	)

	rec := getJobContent(t, fe, jobID)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(
		t, "text/plain",
		rec.Header().Get("Content-Type"),
	)
	assert.Equal(t, "hello world", rec.Body.String())
}

func TestFull_BinaryRoundTrip(t *testing.T) {
	binaryData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0xFF, 0xAA,
	}

	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.Header().Set(
				"Content-Type", "image/png",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(binaryData)
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	jobID := submitAndWait(
		t, fe, http.MethodGet, "/image", nil,
	)

	rec := getJobContent(t, fe, jobID)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(
		t, "image/png",
		rec.Header().Get("Content-Type"),
	)
	assert.Equal(t, binaryData, rec.Body.Bytes())
}

func TestFull_Upstream500IsCompleted(t *testing.T) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(
				http.StatusInternalServerError,
			)
			_, _ = w.Write([]byte("oops"))
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	jobID := submitAndWait(
		t, fe, http.MethodGet, "/fail", nil,
	)

	statusRec := getJobStatus(t, fe, jobID)

	var info jobInfo

	require.NoError(t, json.Unmarshal(
		statusRec.Body.Bytes(), &info,
	))
	assert.Equal(t, StatusCompleted, info.Status)

	contentRec := getJobContent(t, fe, jobID)

	assert.Equal(
		t, http.StatusInternalServerError,
		contentRec.Code,
	)
	assert.Equal(t, "oops", contentRec.Body.String())
}

func TestFull_Upstream404_NoProxqHeader(
	t *testing.T,
) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not here"))
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	jobID := submitAndWait(
		t, fe, http.MethodGet, "/missing", nil,
	)

	rec := getJobContent(t, fe, jobID)

	assert.Equal(
		t, http.StatusNotFound, rec.Code,
	)
	assert.Empty(
		t, rec.Header().Get(HeaderNameXProxqSource),
	)
	assert.Equal(t, "not here", rec.Body.String())
}

func TestFull_ContentPending_HasProxqHeader(
	t *testing.T,
) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			time.Sleep(5 * time.Second)
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	req := httptest.NewRequest(
		http.MethodGet, "/slow", nil,
	)
	rec := httptest.NewRecorder()

	fe.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp acceptedResponse

	require.NoError(t, json.Unmarshal(
		rec.Body.Bytes(), &resp,
	))

	contentRec := getJobContent(t, fe, resp.JobID)

	assert.Equal(
		t, http.StatusNotFound, contentRec.Code,
	)
	assert.Equal(
		t, HeaderValueProxq,
		contentRec.Header().Get(
			HeaderNameXProxqSource,
		),
	)
}

func TestFull_ContentNotFound_HasProxqHeader(
	t *testing.T,
) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	rec := getJobContent(t, fe, "nonexistent-id")

	assert.Equal(
		t, http.StatusNotFound, rec.Code,
	)
	assert.Equal(
		t, HeaderValueProxq,
		rec.Header().Get(HeaderNameXProxqSource),
	)
}

func TestFull_MultipleContentFetches(t *testing.T) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.Header().Set(
				"Content-Type", "text/plain",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("stable"))
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	jobID := submitAndWait(
		t, fe, http.MethodGet, "/stable", nil,
	)

	for range 3 {
		rec := getJobContent(t, fe, jobID)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "stable", rec.Body.String())
	}
}

func TestFull_PostWithBody(t *testing.T) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			body, _ := io.ReadAll(r.Body)

			w.Header().Set(
				"Content-Type", "application/json",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, upstream.URL, nil)
	defer fe.cleanup()

	jobID := submitAndWait(
		t, fe, http.MethodPost, "/echo",
		strings.NewReader(`{"key":"value"}`),
	)

	rec := getJobContent(t, fe, jobID)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(
		t, rec.Body.String(), `{"key":"value"}`,
	)
}

func TestFull_MultiUpstreamRouting(t *testing.T) {
	apiServer := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"Content-Type", "text/plain",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write( //nolint:gosec
				[]byte("api:" + r.URL.Path),
			)
		}),
	)
	defer apiServer.Close()

	defaultServer := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"Content-Type", "text/plain",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write( //nolint:gosec
				[]byte("default:" + r.URL.Path),
			)
		}),
	)
	defer defaultServer.Close()

	fe := setupFull(t, "", []UpstreamConfig{
		{
			Prefix:      "/api",
			URL:         apiServer.URL,
			MaxBodySize: config.DefaultMaxBodySize,
		},
		{
			Prefix:      "/",
			URL:         defaultServer.URL,
			MaxBodySize: config.DefaultMaxBodySize,
		},
	})
	defer fe.cleanup()

	tests := []struct {
		name       string
		path       string
		expectBody string
	}{
		{
			name:       "routes to api",
			path:       "/api/users",
			expectBody: "api:/users",
		},
		{
			name:       "routes to default",
			path:       "/other",
			expectBody: "default:/other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobID := submitAndWait(
				t, fe, http.MethodGet, tt.path, nil,
			)

			rec := getJobContent(t, fe, jobID)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(
				t, tt.expectBody, rec.Body.String(),
			)
		})
	}
}

func TestFull_RetriesOnUpstreamFailure(
	t *testing.T,
) {
	var calls atomic.Int32

	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			n := calls.Add(1)
			if n <= 2 {
				hj, ok := w.(http.Hijacker)
				if !ok {
					return
				}

				conn, _, err := hj.Hijack()
				if err != nil {
					return
				}

				_ = conn.Close()

				return
			}

			w.Header().Set(
				"Content-Type", "text/plain",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success after retries"))
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, "", []UpstreamConfig{
		{
			Prefix:      "/",
			URL:         upstream.URL,
			MaxRetries:  3,
			RetryDelay:  1 * time.Second,
			MaxBodySize: config.DefaultMaxBodySize,
		},
	})
	defer fe.cleanup()

	req := httptest.NewRequest(
		http.MethodGet, "/retry-me", nil,
	)
	rec := httptest.NewRecorder()

	fe.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp acceptedResponse

	require.NoError(t, json.Unmarshal(
		rec.Body.Bytes(), &resp,
	))

	require.Eventually(t, func() bool {
		info, err := fe.inspector.GetTaskInfo(
			fe.queue, resp.JobID,
		)
		if err != nil {
			return false
		}

		return info.State == asynq.TaskStateCompleted
	}, 60*time.Second, 200*time.Millisecond)

	contentRec := getJobContent(t, fe, resp.JobID)

	assert.Equal(t, http.StatusOK, contentRec.Code)
	assert.Equal(
		t, "success after retries",
		contentRec.Body.String(),
	)
	assert.Equal(t, int32(3), calls.Load())
}

func TestFull_RetriesExhausted(t *testing.T) {
	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				return
			}

			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}

			_ = conn.Close()
		}),
	)
	defer upstream.Close()

	fe := setupFull(t, "", []UpstreamConfig{
		{
			Prefix:      "/",
			URL:         upstream.URL,
			MaxRetries:  2,
			RetryDelay:  1 * time.Second,
			MaxBodySize: config.DefaultMaxBodySize,
		},
	})
	defer fe.cleanup()

	req := httptest.NewRequest(
		http.MethodGet, "/always-fail", nil,
	)
	rec := httptest.NewRecorder()

	fe.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp acceptedResponse

	require.NoError(t, json.Unmarshal(
		rec.Body.Bytes(), &resp,
	))

	require.Eventually(t, func() bool {
		info, err := fe.inspector.GetTaskInfo(
			fe.queue, resp.JobID,
		)
		if err != nil {
			return false
		}

		return info.State == asynq.TaskStateArchived
	}, 60*time.Second, 200*time.Millisecond)

	statusRec := getJobStatus(t, fe, resp.JobID)

	var info jobInfo

	require.NoError(t, json.Unmarshal(
		statusRec.Body.Bytes(), &info,
	))
	assert.Equal(t, StatusFailed, info.Status)
}
