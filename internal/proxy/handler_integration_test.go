package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/proxq/internal/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errForcedRead = errors.New("forced read error")

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errForcedRead
}

func (errReader) Close() error { return nil }

func setupRedis(
	t *testing.T,
) (*testinfra.Redis, func()) {
	t.Helper()

	ctx := context.Background()

	redis, err := testinfra.SetupRedis(ctx)
	require.NoError(t, err)

	return redis, func() { redis.Teardown(ctx) }
}

func TestHandler_ServeHTTP(t *testing.T) {
	redis, cleanup := setupRedis(t)
	defer cleanup()

	client := asynq.NewClient(redis.RedisOpt())

	defer func() { _ = client.Close() }()

	inspector := asynq.NewInspector(redis.RedisOpt())

	defer func() { _ = inspector.Close() }()

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		upstream  string
		queue     string
		headers   map[string]string
		expectURL string
	}{
		{
			name:      "POST with body",
			method:    http.MethodPost,
			path:      "/api/data",
			body:      `{"key":"value"}`,
			upstream:  "http://upstream:8080",
			queue:     "q1",
			expectURL: "http://upstream:8080/api/data",
		},
		{
			name:      "GET without body",
			method:    http.MethodGet,
			path:      "/health",
			body:      "",
			upstream:  "http://backend:9090",
			queue:     "q2",
			expectURL: "http://backend:9090/health",
		},
		{
			name:     "PUT with custom headers",
			method:   http.MethodPut,
			path:     "/resource/42",
			body:     "update",
			upstream: "http://svc:80",
			queue:    "q3",
			headers: map[string]string{
				"X-Custom": "val",
			},
			expectURL: "http://svc:80/resource/42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(client, HandlerConfig{
				UpstreamURL:   tt.upstream,
				Queue:         tt.queue,
				TaskRetention: 10 * time.Minute,
			})

			req := httptest.NewRequest(
				tt.method, tt.path,
				strings.NewReader(tt.body),
			)

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(
				t, http.StatusAccepted, rec.Code,
			)

			var resp acceptedResponse

			err := json.Unmarshal(
				rec.Body.Bytes(), &resp,
			)
			require.NoError(t, err)
			assert.NotEmpty(t, resp.JobID)

			info, err := inspector.GetTaskInfo(
				tt.queue, resp.JobID,
			)
			require.NoError(t, err)
			assert.Equal(t, TaskTypeName, info.Type)

			var payload struct {
				Method  string              `json:"method"`
				URL     string              `json:"url"`
				Headers map[string][]string `json:"headers"`
			}

			err = json.Unmarshal(
				info.Payload, &payload,
			)
			require.NoError(t, err)
			assert.Equal(t, tt.method, payload.Method)
			assert.Equal(t, tt.expectURL, payload.URL)

			for k, v := range tt.headers {
				assert.Contains(
					t, payload.Headers[k], v,
				)
			}
		})
	}
}

func TestHandler_ServeHTTP_ReadBodyError(
	t *testing.T,
) {
	h := NewHandler(nil, HandlerConfig{
		UpstreamURL: "http://upstream",
	})

	req := httptest.NewRequest(
		http.MethodPost, "/test", nil,
	)
	req.Body = &errReader{}

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(
		t, http.StatusInternalServerError, rec.Code,
	)
}

func TestHandler_ServeHTTP_EnqueueError(
	t *testing.T,
) {
	badOpt := asynq.RedisClientOpt{
		Addr: "localhost:1",
	}
	client := asynq.NewClient(badOpt)

	defer func() { _ = client.Close() }()

	h := NewHandler(client, HandlerConfig{
		UpstreamURL: "http://upstream",
	})

	req := httptest.NewRequest(
		http.MethodGet, "/test", nil,
	)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(
		t, http.StatusInternalServerError, rec.Code,
	)
}

func TestJobsHandler_Integration(t *testing.T) {
	redis, cleanup := setupRedis(t)
	defer cleanup()

	client := asynq.NewClient(redis.RedisOpt())

	defer func() { _ = client.Close() }()

	inspector := asynq.NewInspector(redis.RedisOpt())

	defer func() { _ = inspector.Close() }()

	queue := "jobs-test"

	task := asynq.NewTask(
		TaskTypeName, []byte(`{"method":"GET"}`),
	)

	info, err := client.Enqueue(
		task,
		asynq.Queue(queue),
		asynq.Retention(10*time.Minute),
	)
	require.NoError(t, err)

	taskID := info.ID

	jobsHandler := NewJobsHandler(
		inspector, queue, nil,
	)

	t.Run("get existing job", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc(
			"GET /__jobs/{id}", jobsHandler.Get,
		)

		req := httptest.NewRequest(
			http.MethodGet,
			"/__jobs/"+taskID,
			nil,
		)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var job JobInfo

		err := json.Unmarshal(
			rec.Body.Bytes(), &job,
		)
		require.NoError(t, err)
		assert.Equal(t, taskID, job.ID)
		assert.Equal(t, StatusQueued, job.Status)
	})

	t.Run("get nonexistent job", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc(
			"GET /__jobs/{id}", jobsHandler.Get,
		)

		req := httptest.NewRequest(
			http.MethodGet,
			"/__jobs/does-not-exist",
			nil,
		)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("cancel existing job", func(t *testing.T) {
		cancelTask := asynq.NewTask(
			TaskTypeName, []byte(`{"method":"DELETE"}`),
		)

		cancelInfo, err := client.Enqueue(
			cancelTask,
			asynq.Queue(queue),
			asynq.Retention(10*time.Minute),
		)
		require.NoError(t, err)

		mux := http.NewServeMux()
		mux.HandleFunc(
			"DELETE /__jobs/{id}",
			jobsHandler.Cancel,
		)

		req := httptest.NewRequest(
			http.MethodDelete,
			"/__jobs/"+cancelInfo.ID,
			nil,
		)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		_, err = inspector.GetTaskInfo(
			queue, cancelInfo.ID,
		)
		assert.ErrorIs(t, err, asynq.ErrTaskNotFound)
	})

	t.Run("cancel nonexistent job", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc(
			"DELETE /__jobs/{id}",
			jobsHandler.Cancel,
		)

		req := httptest.NewRequest(
			http.MethodDelete,
			"/__jobs/nope",
			nil,
		)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}
