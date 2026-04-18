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
	"github.com/psyb0t/aichteeteapee"
	"github.com/psyb0t/proxq/internal/config"
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

func singleUpstreamCfg(
	url, queue string,
) HandlerConfig {
	return HandlerConfig{
		Queue:         queue,
		TaskRetention: 10 * time.Minute,
		Upstreams: []UpstreamConfig{
			{
				Prefix:      "/",
				URL:         url,
				MaxBodySize: config.DefaultMaxBodySize,
			},
		},
	}
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
			h := NewHandler(
				client,
				singleUpstreamCfg(
					tt.upstream, tt.queue,
				),
			)

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

			var envelope struct {
				Request struct {
					Method  string              `json:"method"`
					URL     string              `json:"url"`
					Headers map[string][]string `json:"headers"`
				} `json:"request"`
			}

			err = json.Unmarshal(
				info.Payload, &envelope,
			)
			require.NoError(t, err)
			assert.Equal(
				t, tt.method, envelope.Request.Method,
			)
			assert.Equal(
				t, tt.expectURL, envelope.Request.URL,
			)

			for k, v := range tt.headers {
				assert.Contains(
					t, envelope.Request.Headers[k], v,
				)
			}
		})
	}
}

func TestHandler_ServeHTTP_MultiUpstream(
	t *testing.T,
) {
	redis, cleanup := setupRedis(t)
	defer cleanup()

	client := asynq.NewClient(redis.RedisOpt())

	defer func() { _ = client.Close() }()

	inspector := asynq.NewInspector(redis.RedisOpt())

	defer func() { _ = inspector.Close() }()

	h := NewHandler(client, HandlerConfig{
		Queue:         "multi",
		TaskRetention: 10 * time.Minute,
		Upstreams: []UpstreamConfig{
			{
				Prefix:      "/api",
				URL:         "http://api:3000",
				MaxBodySize: config.DefaultMaxBodySize,
			},
			{
				Prefix:      "/",
				URL:         "http://default:8080",
				MaxBodySize: config.DefaultMaxBodySize,
			},
		},
	})

	tests := []struct {
		name      string
		path      string
		expectURL string
	}{
		{
			name:      "routes to /api upstream",
			path:      "/api/users",
			expectURL: "http://api:3000/users",
		},
		{
			name:      "routes to default upstream",
			path:      "/other",
			expectURL: "http://default:8080/other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet, tt.path, nil,
			)
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(
				t, http.StatusAccepted, rec.Code,
			)

			var resp acceptedResponse

			require.NoError(t, json.Unmarshal(
				rec.Body.Bytes(), &resp,
			))

			info, err := inspector.GetTaskInfo(
				"multi", resp.JobID,
			)
			require.NoError(t, err)

			var envelope struct {
				Request struct {
					URL string `json:"url"`
				} `json:"request"`
			}

			require.NoError(t, json.Unmarshal(
				info.Payload, &envelope,
			))
			assert.Equal(
				t, tt.expectURL, envelope.Request.URL,
			)
		})
	}
}

func TestHandler_CacheKeyExcludeHeaders(
	t *testing.T,
) {
	redis, cleanup := setupRedis(t)
	defer cleanup()

	client := asynq.NewClient(redis.RedisOpt())

	defer func() { _ = client.Close() }()

	inspector := asynq.NewInspector(redis.RedisOpt())

	defer func() { _ = inspector.Close() }()

	queue := "cache-excl"
	excludeHeaders := []string{
		"Authorization", "X-Trace-ID",
	}

	h := NewHandler(client, HandlerConfig{
		Queue:         queue,
		TaskRetention: 10 * time.Minute,
		Upstreams: []UpstreamConfig{
			{
				Prefix:                 "/",
				URL:                    "http://backend:8080",
				MaxBodySize:            config.DefaultMaxBodySize,
				CacheKeyExcludeHeaders: excludeHeaders,
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodGet, "/test", nil,
	)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp acceptedResponse

	require.NoError(t, json.Unmarshal(
		rec.Body.Bytes(), &resp,
	))

	info, err := inspector.GetTaskInfo(
		queue, resp.JobID,
	)
	require.NoError(t, err)

	var envelope struct {
		CacheKeyExcludeHeaders []string `json:"cacheKeyExcludeHeaders"` //nolint:lll
	}

	require.NoError(t, json.Unmarshal(
		info.Payload, &envelope,
	))
	assert.Equal(
		t, excludeHeaders,
		envelope.CacheKeyExcludeHeaders,
	)
}

func TestHandler_ServeHTTP_NoUpstreamMatch(
	t *testing.T,
) {
	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{
				Prefix:      "/api",
				URL:         "http://api:3000",
				MaxBodySize: config.DefaultMaxBodySize,
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodGet, "/other", nil,
	)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(
		t, http.StatusBadGateway, rec.Code,
	)
}

func TestHandler_ServeHTTP_WebSocketProxy(
	t *testing.T,
) {
	upgraded := make(chan struct{})

	upstreamSrv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			if strings.EqualFold(
				r.Header.Get(
					aichteeteapee.HeaderNameUpgrade,
				),
				"websocket",
			) {
				close(upgraded)
				w.WriteHeader(
					http.StatusSwitchingProtocols,
				)

				return
			}

			w.WriteHeader(http.StatusOK)
		}),
	)
	defer upstreamSrv.Close()

	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{Prefix: "/", URL: upstreamSrv.URL},
		},
	})

	req := httptest.NewRequest(
		http.MethodGet, "/ws", nil,
	)
	req.Header.Set(
		aichteeteapee.HeaderNameConnection, "upgrade",
	)
	req.Header.Set(
		aichteeteapee.HeaderNameUpgrade, "websocket",
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	select {
	case <-upgraded:
	default:
		t.Error("upstream did not receive WS upgrade")
	}
}

func TestHandler_ServeHTTP_DirectProxyChunked(
	t *testing.T,
) {
	received := make(chan string, 1)

	upstreamSrv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			received <- r.Method + " " + r.URL.Path

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("proxied"))
		}),
	)
	defer upstreamSrv.Close()

	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{Prefix: "/", URL: upstreamSrv.URL},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost, "/upload",
		strings.NewReader("data"),
	)
	req.TransferEncoding = []string{"chunked"}

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "proxied", rec.Body.String())

	select {
	case got := <-received:
		assert.Equal(t, "POST /upload", got)
	default:
		t.Error("upstream did not receive request")
	}
}

func TestHandler_ServeHTTP_DirectProxyLargeBody(
	t *testing.T,
) {
	received := make(chan bool, 1)

	upstreamSrv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			received <- true

			w.WriteHeader(http.StatusOK)
		}),
	)
	defer upstreamSrv.Close()

	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{
				Prefix:               "/",
				URL:                  upstreamSrv.URL,
				DirectProxyThreshold: 100,
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost, "/big",
		strings.NewReader(strings.Repeat("x", 200)),
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	select {
	case <-received:
	default:
		t.Error("upstream did not receive request")
	}
}

func TestHandler_ServeHTTP_ReadBodyError(
	t *testing.T,
) {
	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{
				Prefix:      "/",
				URL:         "http://upstream",
				MaxBodySize: config.DefaultMaxBodySize,
			},
		},
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
		Upstreams: []UpstreamConfig{
			{
				Prefix:      "/",
				URL:         "http://upstream",
				MaxBodySize: config.DefaultMaxBodySize,
			},
		},
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

func TestHandler_DirectProxyRedirectMode(
	t *testing.T,
) {
	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{
				Prefix:               "/",
				URL:                  "http://upstream:3000/base",
				DirectProxyThreshold: 10,
				DirectProxyMode:      config.DirectProxyModeRedirect,
				MaxBodySize:          config.DefaultMaxBodySize,
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost, "/path?q=1",
		strings.NewReader(strings.Repeat("x", 20)),
	)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(
		t, http.StatusTemporaryRedirect, rec.Code,
	)
	assert.Equal(
		t, "http://upstream:3000/base/path?q=1",
		rec.Header().Get("Location"),
	)
}

func TestHandler_DirectProxyRedirect_PrefixStrip(
	t *testing.T,
) {
	h := NewHandler(nil, HandlerConfig{
		Upstreams: []UpstreamConfig{
			{
				Prefix:               "/api",
				URL:                  "http://backend:3000",
				DirectProxyThreshold: 10,
				DirectProxyMode:      config.DirectProxyModeRedirect,
				MaxBodySize:          config.DefaultMaxBodySize,
			},
			{
				Prefix:      "/",
				URL:         "http://default:8080",
				MaxBodySize: config.DefaultMaxBodySize,
			},
		},
	})

	req := httptest.NewRequest(
		http.MethodPost, "/api/users?page=2",
		strings.NewReader(strings.Repeat("x", 20)),
	)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(
		t, http.StatusTemporaryRedirect, rec.Code,
	)
	assert.Equal(
		t, "http://backend:3000/users?page=2",
		rec.Header().Get("Location"),
	)
}

func TestHandler_WebSocketNilProxy(t *testing.T) {
	h := &Handler{
		upstreams: []upstream{
			{
				prefix:       "/",
				reverseProxy: nil,
			},
		},
	}

	req := httptest.NewRequest(
		http.MethodGet, "/ws", nil,
	)
	req.Header.Set(
		aichteeteapee.HeaderNameConnection, "upgrade",
	)
	req.Header.Set(
		aichteeteapee.HeaderNameUpgrade, "websocket",
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(
		t, http.StatusBadGateway, rec.Code,
	)
}

func TestHandler_DirectProxyNilReverseProxy(
	t *testing.T,
) {
	h := &Handler{
		upstreams: []upstream{
			{
				prefix:               "/",
				directProxyThreshold: 10,
				directProxyMode:      config.DirectProxyModeProxy,
				reverseProxy:         nil,
			},
		},
	}

	req := httptest.NewRequest(
		http.MethodPost, "/big",
		strings.NewReader(strings.Repeat("x", 20)),
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	assert.Equal(
		t, http.StatusBadGateway, rec.Code,
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
		inspector, queue,
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

		var job jobInfo

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
		assert.Equal(
			t, HeaderValueProxq,
			rec.Header().Get(HeaderNameXProxqSource),
		)
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

	t.Run("content pending has proxq source header",
		func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(
				"GET /__jobs/{id}/content",
				jobsHandler.Content,
			)

			req := httptest.NewRequest(
				http.MethodGet,
				"/__jobs/"+taskID+"/content",
				nil,
			)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(
				t, http.StatusNotFound, rec.Code,
			)
			assert.Equal(
				t, HeaderValueProxq,
				rec.Header().Get(
					HeaderNameXProxqSource,
				),
			)
		})

	t.Run("content nonexistent has proxq source header",
		func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(
				"GET /__jobs/{id}/content",
				jobsHandler.Content,
			)

			req := httptest.NewRequest(
				http.MethodGet,
				"/__jobs/nope/content",
				nil,
			)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			assert.Equal(
				t, http.StatusNotFound, rec.Code,
			)
			assert.Equal(
				t, HeaderValueProxq,
				rec.Header().Get(
					HeaderNameXProxqSource,
				),
			)
		})

	t.Run("cancel nonexistent has proxq source header",
		func(t *testing.T) {
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

			assert.Equal(
				t, http.StatusNotFound, rec.Code,
			)
			assert.Equal(
				t, HeaderValueProxq,
				rec.Header().Get(
					HeaderNameXProxqSource,
				),
			)
		})
}
