package openai

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

	proxqtypes "github.com/psyb0t/docker-proxq/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeProxq(
	t *testing.T,
	upstreamBody string,
	upstreamStatus int,
	upstreamHeaders map[string]string,
) *httptest.Server {
	t.Helper()

	jobID := "test-job-123"

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /v1/chat/completions",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.Header().Set(
				"Content-Type", "application/json",
			)
			w.WriteHeader(http.StatusAccepted)

			resp, _ := json.Marshal(
				jobAccepted{JobID: jobID},
			)
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /__jobs/{id}",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")

			w.Header().Set(
				"Content-Type", "application/json",
			)

			resp, _ := json.Marshal(jobStatus{
				ID:     id,
				Status: "completed",
			})
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /__jobs/{id}/content",
		func(w http.ResponseWriter, _ *http.Request) {
			for k, v := range upstreamHeaders {
				w.Header().Set(k, v)
			}

			w.WriteHeader(upstreamStatus)
			_, _ = w.Write([]byte(upstreamBody))
		},
	)

	return httptest.NewServer(mux)
}

func postCompletion(
	t *testing.T,
	transport *proxqTransport,
	targetURL string,
	body string,
) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequest(
		http.MethodPost,
		targetURL,
		strings.NewReader(body),
	)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	return transport.RoundTrip(req)
}

func requireCloseBody(
	t *testing.T,
	resp *http.Response,
) {
	t.Helper()

	if resp == nil || resp.Body == nil {
		return
	}

	require.NoError(t, resp.Body.Close())
}

func TestTransport_QueuedRoundTrip(t *testing.T) {
	srv := fakeProxq(
		t,
		`{"id":"chatcmpl-1","choices":[]}`,
		http.StatusOK,
		map[string]string{
			"Content-Type": "application/json",
		},
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o","messages":[]}`,
	)
	require.NoError(t, err)

	defer requireCloseBody(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(respBody), "chatcmpl-1")
}

func TestTransport_DirectPassthrough(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.Header().Set(
				"Content-Type", "application/json",
			)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(
				[]byte(`{"direct":"response"}`),
			)
		}),
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	req, err := http.NewRequest(
		http.MethodGet, srv.URL+"/health", nil,
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)

	defer requireCloseBody(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, `{"direct":"response"}`, string(body))
}

func TestTransport_StreamingQueues(
	t *testing.T,
) {
	srv := fakeProxq(
		t,
		`{"id":"chatcmpl-stream"}`,
		http.StatusOK,
		map[string]string{
			"Content-Type": "application/json",
		},
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o","stream":true}`,
	)
	require.NoError(t, err)

	defer requireCloseBody(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(
		t, string(respBody), "chatcmpl-stream",
	)
}

func TestTransport_JobFailed(t *testing.T) {
	jobID := "fail-job-456"

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /v1/chat/completions",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.WriteHeader(http.StatusAccepted)

			resp, _ := json.Marshal(
				jobAccepted{JobID: jobID},
			)
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /__jobs/{id}",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")

			w.Header().Set(
				"Content-Type", "application/json",
			)

			resp, _ := json.Marshal(jobStatus{
				ID:     id,
				Status: "failed",
			})
			_, _ = w.Write(resp)
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o"}`,
	)

	defer requireCloseBody(t, resp)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrJobFailed)
}

func TestTransport_PollsUntilCompleted(t *testing.T) {
	var pollCount atomic.Int32

	jobID := "poll-job-789"

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /v1/chat/completions",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.WriteHeader(http.StatusAccepted)

			resp, _ := json.Marshal(
				jobAccepted{JobID: jobID},
			)
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /__jobs/{id}",
		func(w http.ResponseWriter, r *http.Request) {
			n := pollCount.Add(1)
			id := r.PathValue("id")

			status := "running"
			if n >= 3 {
				status = "completed"
			}

			w.Header().Set(
				"Content-Type", "application/json",
			)

			resp, _ := json.Marshal(jobStatus{
				ID:     id,
				Status: status,
			})
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /__jobs/{id}/content",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(
				"Content-Type", "application/json",
			)
			_, _ = w.Write([]byte(`{"done":true}`))
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o"}`,
	)
	require.NoError(t, err)

	defer requireCloseBody(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, pollCount.Load(), int32(3))
}

func TestTransport_EmptyJobID(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"jobId":""}`))
		}),
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/test",
		`{}`,
	)

	defer requireCloseBody(t, resp)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyJobID)
}

func TestTransport_UpstreamHeadersPreserved(
	t *testing.T,
) {
	srv := fakeProxq(
		t,
		`{"result":"ok"}`,
		http.StatusOK,
		map[string]string{
			"Content-Type":     "application/json",
			"X-Custom":         "upstream-value",
			"X-RateLimit-Left": "42",
		},
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o"}`,
	)
	require.NoError(t, err)

	defer requireCloseBody(t, resp)

	assert.Equal(
		t, "upstream-value",
		resp.Header.Get("X-Custom"),
	)
	assert.Equal(
		t, "42",
		resp.Header.Get("X-RateLimit-Left"),
	)
}

func TestTransport_CustomJobsPath(t *testing.T) {
	jobID := "custom-path-job"

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /v1/chat/completions",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.WriteHeader(http.StatusAccepted)

			resp, _ := json.Marshal(
				jobAccepted{JobID: jobID},
			)
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /custom-jobs/{id}",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")

			resp, _ := json.Marshal(jobStatus{
				ID:     id,
				Status: "completed",
			})
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /custom-jobs/{id}/content",
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"custom":true}`))
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		JobsPath:     "/custom-jobs",
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o"}`,
	)
	require.NoError(t, err)

	defer requireCloseBody(t, resp)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), `"custom":true`)
}

func TestBuildTransport_Defaults(t *testing.T) {
	transport := buildTransport(Config{
		ProxqBaseURL: "http://localhost:8080",
	})

	assert.Equal(
		t, "http://localhost:8080/__jobs",
		transport.jobsBaseURL.String(),
	)
	assert.Equal(
		t, defaultPollInterval, transport.pollInterval,
	)
	assert.NotNil(t, transport.httpClient)
}

func TestBuildTransport_CustomConfig(t *testing.T) {
	customClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	transport := buildTransport(Config{
		ProxqBaseURL: "http://proxq:9090",
		JobsPath:     "/status",
		PollInterval: 2 * time.Second,
		Timeout:      5 * time.Minute,
		HTTPClient:   customClient,
	})

	assert.Equal(
		t, "http://proxq:9090/status",
		transport.jobsBaseURL.String(),
	)
	assert.Equal(
		t, 2*time.Second, transport.pollInterval,
	)
	assert.Equal(t, 5*time.Minute, transport.timeout)
	assert.Equal(t, customClient, transport.httpClient)
}

func TestTransport_TimeoutHeaderSent(t *testing.T) {
	var gotHeader string

	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			gotHeader = r.Header.Get(
				proxqtypes.HeaderNameXProxqTimeout,
			)

			w.WriteHeader(http.StatusOK)
		}),
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		Timeout:      2*time.Minute + 30*time.Second,
		HTTPClient:   srv.Client(),
	})

	req, err := http.NewRequest(
		http.MethodPost,
		srv.URL+"/v1/chat/completions",
		strings.NewReader(`{}`),
	)
	require.NoError(t, err)

	resp, _ := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	assert.Equal(t, "2m30s", gotHeader)
}

func TestTransport_NoTimeoutHeaderWhenZero(t *testing.T) {
	var gotHeader string

	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			gotHeader = r.Header.Get(
				proxqtypes.HeaderNameXProxqTimeout,
			)

			w.WriteHeader(http.StatusOK)
		}),
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		HTTPClient:   srv.Client(),
	})

	req, err := http.NewRequest(
		http.MethodPost,
		srv.URL+"/v1/chat/completions",
		strings.NewReader(`{}`),
	)
	require.NoError(t, err)

	resp, _ := transport.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	assert.Empty(t, gotHeader)
}

func TestTransport_ContextCancellation(t *testing.T) {
	jobID := "cancel-job"

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /v1/chat/completions",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.WriteHeader(http.StatusAccepted)

			resp, _ := json.Marshal(
				jobAccepted{JobID: jobID},
			)
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /__jobs/{id}",
		func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")

			resp, _ := json.Marshal(jobStatus{
				ID:     id,
				Status: "running",
			})
			_, _ = w.Write(resp)
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	ctx, cancel := context.WithTimeout(
		context.Background(), 100*time.Millisecond,
	)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o"}`),
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)

	defer requireCloseBody(t, resp)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTransport_UpstreamErrorStatusPreserved(
	t *testing.T,
) {
	srv := fakeProxq(
		t,
		`{"error":"rate limited"}`,
		http.StatusTooManyRequests,
		map[string]string{
			"Content-Type":     "application/json",
			"Retry-After":      "30",
			"X-RateLimit-Left": "0",
		},
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o"}`,
	)
	require.NoError(t, err)

	defer requireCloseBody(t, resp)

	assert.Equal(
		t, http.StatusTooManyRequests, resp.StatusCode,
	)
	assert.Equal(
		t, "30", resp.Header.Get("Retry-After"),
	)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(
		t, string(body), "rate limited",
	)
}

func TestNewClient(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer srv.Close()

	client := NewClient(Config{
		ProxqBaseURL: srv.URL,
		APIKey:       "sk-test",
		PollInterval: 10 * time.Millisecond,
	})

	assert.NotNil(t, client)
}

func TestNewClient_WithHTTPClient(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer srv.Close()

	customClient := &http.Client{
		Timeout: 42 * time.Second,
	}

	client := NewClient(Config{
		ProxqBaseURL: srv.URL,
		HTTPClient:   customClient,
	})

	assert.NotNil(t, client)
}

func TestNewClient_NoAPIKey(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer srv.Close()

	client := NewClient(Config{
		ProxqBaseURL: srv.URL,
	})

	assert.NotNil(t, client)
}

func TestCloneClientWithTransport_NilBase(
	t *testing.T,
) {
	rt := http.DefaultTransport

	result := cloneClientWithTransport(nil, rt)

	assert.NotNil(t, result)
	assert.Equal(t, rt, result.Transport)
	assert.Zero(t, result.Timeout)
	assert.Nil(t, result.Jar)
}

func TestCloneClientWithTransport_WithBase(
	t *testing.T,
) {
	rt := http.DefaultTransport

	base := &http.Client{
		Timeout: 99 * time.Second,
	}

	result := cloneClientWithTransport(base, rt)

	assert.Equal(t, rt, result.Transport)
	assert.Equal(t, 99*time.Second, result.Timeout)
}

func TestTransport_ProxqDown(t *testing.T) {
	transport := buildTransport(Config{
		ProxqBaseURL: "http://127.0.0.1:1",
		PollInterval: 10 * time.Millisecond,
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:1/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o"}`),
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)

	defer requireCloseBody(t, resp)

	require.Error(t, err)
}

func TestTransport_InvalidJobResponse(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`not json`))
		}),
	)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o"}`,
	)

	defer requireCloseBody(t, resp)

	require.Error(t, err)
}

func TestTransport_PollInvalidJSON(t *testing.T) {
	jobID := "bad-status-job"

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /v1/chat/completions",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(
				proxqtypes.HeaderNameXProxqSource,
				proxqtypes.HeaderValueProxq,
			)
			w.WriteHeader(http.StatusAccepted)

			resp, _ := json.Marshal(
				jobAccepted{JobID: jobID},
			)
			_, _ = w.Write(resp)
		},
	)

	mux.HandleFunc(
		"GET /__jobs/{id}",
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{broken`))
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	transport := buildTransport(Config{
		ProxqBaseURL: srv.URL,
		PollInterval: 10 * time.Millisecond,
		HTTPClient:   srv.Client(),
	})

	resp, err := postCompletion(
		t, transport,
		srv.URL+"/v1/chat/completions",
		`{"model":"gpt-4o"}`,
	)

	defer requireCloseBody(t, resp)

	require.Error(t, err)
}
