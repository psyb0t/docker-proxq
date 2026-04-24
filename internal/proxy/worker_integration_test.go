package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	prawxxey "github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	"github.com/psyb0t/docker-proxq/internal/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowUpstream returns a test server that sleeps for d before responding.
func slowUpstream(d time.Duration) *httptest.Server {
	return httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			select {
			case <-time.After(d):
				w.WriteHeader(http.StatusOK)
			case <-r.Context().Done():
			}
		}),
	)
}

func makeTask(t *testing.T, url string) *asynq.Task {
	t.Helper()

	data, err := json.Marshal(taskEnvelope{
		Request: prawxxey.RequestPayload{
			Method: http.MethodGet,
			URL:    url,
		},
	})
	require.NoError(t, err)

	return asynq.NewTask(TaskTypeName, data)
}

// TestWorker_ProcessTask_ContextDeadlineKillsRequest verifies that when
// http.Client.Timeout is 0 (no hardcoded timeout), a context deadline
// cancels the upstream HTTP request at the right time.
func TestWorker_ProcessTask_ContextDeadlineKillsRequest(
	t *testing.T,
) {
	upstream := slowUpstream(2 * time.Second)
	defer upstream.Close()

	w := NewWorker(WorkerConfig{}) // UpstreamTimeout=0, relies on ctx

	ctx, cancel := context.WithTimeout(
		context.Background(), 200*time.Millisecond,
	)
	defer cancel()

	start := time.Now()

	err := w.ProcessTask(ctx, makeTask(t, upstream.URL))

	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, time.Second,
		"should have been killed by context at ~200ms, not after %s", elapsed,
	)
}

// TestWorker_ProcessTask_UpstreamTimeoutKillsRequest verifies that an
// explicit UpstreamTimeout (http.Client.Timeout) also cancels slow requests.
func TestWorker_ProcessTask_UpstreamTimeoutKillsRequest(
	t *testing.T,
) {
	upstream := slowUpstream(2 * time.Second)
	defer upstream.Close()

	w := NewWorker(WorkerConfig{
		UpstreamTimeout: 200 * time.Millisecond,
	})

	start := time.Now()

	err := w.ProcessTask(
		context.Background(), makeTask(t, upstream.URL),
	)

	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, time.Second,
		"should have been killed by http.Client.Timeout at ~200ms, not after %s", elapsed,
	)
}

func TestWorker_ProcessTask_InvalidPayload(
	t *testing.T,
) {
	w := NewWorker(WorkerConfig{})

	task := asynq.NewTask(
		TaskTypeName, []byte("not json"),
	)

	err := w.ProcessTask(
		context.Background(), task,
	)

	assert.Error(t, err)
}

func TestWorker_ProcessTask_UpstreamError(
	t *testing.T,
) {
	w := NewWorker(WorkerConfig{})

	envelope := taskEnvelope{
		Request: prawxxey.RequestPayload{
			Method: http.MethodGet,
			URL:    "http://localhost:1/nope",
		},
	}

	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	task := asynq.NewTask(TaskTypeName, data)

	err = w.ProcessTask(
		context.Background(), task,
	)

	assert.Error(t, err)
}

func TestWorker_ProcessTask_FullRoundTrip(
	t *testing.T,
) {
	var mu sync.Mutex

	received := map[string]string{}

	upstream := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			mu.Lock()
			received[r.URL.Path] = r.Method
			mu.Unlock()

			w.Header().Set(
				"Content-Type", "application/json",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
	)
	defer upstream.Close()

	ctx := context.Background()

	redis, err := testinfra.SetupRedis(ctx)
	require.NoError(t, err)

	defer redis.Teardown(ctx)

	client := asynq.NewClient(redis.RedisOpt())

	defer func() { _ = client.Close() }()

	inspector := asynq.NewInspector(redis.RedisOpt())

	defer func() { _ = inspector.Close() }()

	queue := "worker-test"

	worker := NewWorker(WorkerConfig{
		UpstreamTimeout: 10 * time.Second,
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(TaskTypeName, worker.ProcessTask)

	srv := asynq.NewServer(
		redis.RedisOpt(),
		asynq.Config{
			Concurrency: 1,
			Queues:      map[string]int{queue: 1},
		},
	)

	go func() { _ = srv.Run(mux) }()

	defer srv.Shutdown()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "GET request",
			method: http.MethodGet,
			path:   "/api/test",
		},
		{
			name:   "POST request",
			method: http.MethodPost,
			path:   "/api/data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envelope := taskEnvelope{
				Request: prawxxey.RequestPayload{
					Method: tt.method,
					URL:    upstream.URL + tt.path,
				},
			}

			data, err := json.Marshal(envelope)
			require.NoError(t, err)

			task := asynq.NewTask(TaskTypeName, data)

			info, err := client.Enqueue(
				task,
				asynq.Queue(queue),
				asynq.Retention(5*time.Minute),
			)
			require.NoError(t, err)

			require.Eventually(t, func() bool {
				ti, err := inspector.GetTaskInfo(
					queue, info.ID,
				)
				if err != nil {
					return false
				}

				return ti.State == asynq.TaskStateCompleted
			}, 10*time.Second, 100*time.Millisecond)

			ti, err := inspector.GetTaskInfo(
				queue, info.ID,
			)
			require.NoError(t, err)
			assert.NotEmpty(t, ti.Result)

			var result prawxxey.ResponseResult

			err = json.Unmarshal(ti.Result, &result)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, result.StatusCode)

			mu.Lock()
			assert.Equal(
				t, tt.method, received[tt.path],
			)
			mu.Unlock()
		})
	}
}
