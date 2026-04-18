package tests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

const proxqImage = "proxq:e2e-test"

// upstreamEcho is the response shape from our test
// upstream server.
type upstreamEcho struct {
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Headers      map[string]string `json:"headers"`
	Body         string            `json:"body"`
	RequestCount int               `json:"requestCount"`
}

type jobResponse struct {
	JobID string `json:"jobId"`
}

type jobInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Result *struct {
		StatusCode int                 `json:"statusCode"`
		Headers    map[string][]string `json:"headers"`
		Body       json.RawMessage     `json:"body"`
	} `json:"result"`
	Error string `json:"error"`
}

// upstreamCountsURL returns the URL to get request
// counts from the upstream container.
func upstreamCountsURL(
	ctx context.Context,
	t *testing.T,
	c testcontainers.Container,
) string {
	t.Helper()

	host, err := c.Host(ctx)
	require.NoError(t, err)

	port, err := c.MappedPort(ctx, "3000")
	require.NoError(t, err)

	return fmt.Sprintf(
		"http://%s:%s", host, port.Port(),
	)
}

// decodeUpstreamEcho decodes the result body. The body
// is []byte in ResponseResult which gets base64-encoded
// in JSON, so we unwrap that first.
func decodeUpstreamEcho(
	t *testing.T,
	raw json.RawMessage,
) upstreamEcho {
	t.Helper()

	// raw is a JSON string containing base64.
	var b64 string

	require.NoError(t, json.Unmarshal(raw, &b64))

	decoded, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)

	var echo upstreamEcho

	require.NoError(t, json.Unmarshal(decoded, &echo))

	return echo
}

func getRequestCounts(
	t *testing.T,
	countsURL string,
) map[string]int {
	t.Helper()

	resp, err := http.Get(countsURL + "/request-counts")
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	var counts map[string]int

	require.NoError(t, json.NewDecoder(
		resp.Body,
	).Decode(&counts))

	return counts
}

type env struct {
	proxqURL    string
	countsURL   string
	cleanup     func()
}

func setup(
	t *testing.T,
	cacheMode string,
) env {
	t.Helper()

	ctx := context.Background()

	net, err := network.New(ctx)
	require.NoError(t, err)

	// Redis.
	redis, err := tcredis.Run(
		ctx, "redis:7-alpine",
		network.WithNetwork(
			[]string{"redis"}, net,
		),
	)
	require.NoError(t, err)

	// Upstream Node.js echo server.
	upstream, err := testcontainers.GenericContainer(
		ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "node:22-alpine",
				ExposedPorts: []string{"3000/tcp"},
				Networks:     []string{net.Name},
				NetworkAliases: map[string][]string{
					net.Name: {"upstream"},
				},
				Files: []testcontainers.ContainerFile{
					{
						HostFilePath:      ".fixtures/upstream.js",
						ContainerFilePath: "/app/server.js",
						FileMode:          0o644,
					},
				},
				Cmd: []string{
					"node", "/app/server.js",
				},
				WaitingFor: wait.ForListeningPort(
					"3000/tcp",
				).WithStartupTimeout(
					30 * time.Second,
				),
			},
			Started: true,
		},
	)
	require.NoError(t, err)

	countsURL := upstreamCountsURL(
		ctx, t, upstream,
	)

	// proxq.
	envVars := map[string]string{
		"PROXQ_UPSTREAM_URL":       "http://upstream:3000",
		"PROXQ_REDIS_ADDR":        "redis:6379",
		"PROXQ_CONCURRENCY":       "5",
		"CACHE_MODE":              cacheMode,
		"CACHE_TTL":               "30s",
		"CACHE_MAX_ENTRIES":       "1000",
		"HTTP_SERVER_LISTENADDRESS": "0.0.0.0:8080",
	}

	if cacheMode == "redis" {
		envVars["CACHE_REDIS_ADDR"] = "redis:6379"
	}

	proxq, err := testcontainers.GenericContainer(
		ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        proxqImage,
				ExposedPorts: []string{"8080/tcp"},
				Networks:     []string{net.Name},
				Env:          envVars,
				WaitingFor: wait.ForHTTP("/").
					WithPort("8080/tcp").
					WithStatusCodeMatcher(
						func(status int) bool {
							return status > 0
						},
					).
					WithStartupTimeout(
						30 * time.Second,
					),
			},
			Started: true,
		},
	)
	require.NoError(t, err)

	proxqHost, err := proxq.Host(ctx)
	require.NoError(t, err)

	proxqPort, err := proxq.MappedPort(ctx, "8080")
	require.NoError(t, err)

	proxqURL := fmt.Sprintf(
		"http://%s:%s", proxqHost, proxqPort.Port(),
	)

	return env{
		proxqURL:  proxqURL,
		countsURL: countsURL,
		cleanup: func() {
			_ = proxq.Terminate(ctx)
			_ = upstream.Terminate(ctx)
			_ = redis.Terminate(ctx)
			_ = net.Remove(ctx)
		},
	}
}

func pollJob(
	t *testing.T,
	proxqURL, jobID string,
	timeout time.Duration,
) jobInfo {
	t.Helper()

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(
			proxqURL + "/__jobs/" + jobID,
		)
		if err != nil {
			time.Sleep(200 * time.Millisecond)

			continue
		}

		var info jobInfo

		err = json.NewDecoder(resp.Body).Decode(&info)
		_ = resp.Body.Close()

		if err != nil {
			time.Sleep(200 * time.Millisecond)

			continue
		}

		if info.Status == "completed" ||
			info.Status == "failed" {
			return info
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf(
		"job %s did not complete within %v",
		jobID, timeout,
	)

	return jobInfo{}
}

func submitAndPoll(
	t *testing.T,
	proxqURL, method, path string,
	body io.Reader,
) jobInfo {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequestWithContext(
		context.Background(),
		method,
		proxqURL+path,
		body,
	)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var jr jobResponse

	require.NoError(t, json.NewDecoder(
		resp.Body,
	).Decode(&jr))
	require.NotEmpty(t, jr.JobID)

	return pollJob(
		t, proxqURL, jr.JobID, 30*time.Second,
	)
}

func TestMain(m *testing.M) {
	cmd := exec.Command(
		"docker", "build",
		"-t", proxqImage,
		"../",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf(
			"docker build failed: %s\n%s\n",
			err, string(out),
		)

		os.Exit(1)
	}

	os.Exit(m.Run())
}

func TestE2E_JobLifecycle(t *testing.T) {
	e := setup(t, "none")
	defer e.cleanup()

	info := submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/hello",
		nil,
	)

	assert.Equal(t, "completed", info.Status)
	require.NotNil(t, info.Result)
	assert.Equal(
		t, http.StatusOK,
		info.Result.StatusCode,
	)

	echo := decodeUpstreamEcho(t, info.Result.Body)
	assert.Equal(t, http.MethodGet, echo.Method)
	assert.Contains(t, echo.Path, "/hello")
}

func TestE2E_HeadersForwarded(t *testing.T) {
	e := setup(t, "none")
	defer e.cleanup()

	info := submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/headers",
		nil,
	)

	require.NotNil(t, info.Result)

	echo := decodeUpstreamEcho(t, info.Result.Body)

	// Node.js lowercases all header names.
	key := strings.ToLower(
		aichteeteapee.HeaderNameXForwardedProto,
	)
	assert.NotEmpty(
		t, echo.Headers[key],
		"X-Forwarded-Proto missing",
	)
}

func TestE2E_PostWithBody(t *testing.T) {
	e := setup(t, "none")
	defer e.cleanup()

	body := strings.NewReader(`{"test":"data"}`)

	info := submitAndPoll(
		t, e.proxqURL,
		http.MethodPost, "/echo",
		body,
	)

	require.NotNil(t, info.Result)

	echo := decodeUpstreamEcho(t, info.Result.Body)
	assert.Equal(t, http.MethodPost, echo.Method)
	assert.Contains(t, echo.Body, `{"test":"data"}`)
}

func TestE2E_UpstreamError(t *testing.T) {
	e := setup(t, "none")
	defer e.cleanup()

	info := submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/status/503",
		nil,
	)

	assert.Equal(t, "completed", info.Status)
	require.NotNil(t, info.Result)
	assert.Equal(
		t, http.StatusServiceUnavailable,
		info.Result.StatusCode,
	)
}

func TestE2E_JobNotFound(t *testing.T) {
	e := setup(t, "none")
	defer e.cleanup()

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(
		e.proxqURL + "/__jobs/nonexistent-id",
	)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestE2E_MemoryCacheHit(t *testing.T) {
	e := setup(t, "memory")
	defer e.cleanup()

	// First request — cache miss, hits upstream.
	info1 := submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/cached-endpoint",
		nil,
	)

	require.NotNil(t, info1.Result)
	assert.Equal(
		t, http.StatusOK, info1.Result.StatusCode,
	)

	// Second request — should be a cache hit.
	info2 := submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/cached-endpoint",
		nil,
	)

	require.NotNil(t, info2.Result)

	// Upstream should have been hit only once.
	counts := getRequestCounts(t, e.countsURL)
	assert.Equal(
		t, 1, counts["/cached-endpoint"],
		"expected 1 upstream hit, got %d",
		counts["/cached-endpoint"],
	)

	// Both results should have the same body.
	assert.Equal(
		t,
		string(info1.Result.Body),
		string(info2.Result.Body),
	)
}

func TestE2E_CacheSkipsPost(t *testing.T) {
	e := setup(t, "memory")
	defer e.cleanup()

	submitAndPoll(
		t, e.proxqURL,
		http.MethodPost, "/no-cache-post",
		strings.NewReader("a"),
	)

	submitAndPoll(
		t, e.proxqURL,
		http.MethodPost, "/no-cache-post",
		strings.NewReader("b"),
	)

	counts := getRequestCounts(t, e.countsURL)
	assert.Equal(
		t, 2, counts["/no-cache-post"],
		"POST should not be cached",
	)
}

func TestE2E_CacheSkips5xx(t *testing.T) {
	e := setup(t, "memory")
	defer e.cleanup()

	submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/status/500",
		nil,
	)

	submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/status/500",
		nil,
	)

	counts := getRequestCounts(t, e.countsURL)
	assert.Equal(
		t, 2, counts["/status/500"],
		"5xx should not be cached",
	)
}

func TestE2E_RedisCacheHit(t *testing.T) {
	e := setup(t, "redis")
	defer e.cleanup()

	submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/redis-cached",
		nil,
	)

	submitAndPoll(
		t, e.proxqURL,
		http.MethodGet, "/redis-cached",
		nil,
	)

	counts := getRequestCounts(t, e.countsURL)
	assert.Equal(
		t, 1, counts["/redis-cached"],
		"expected 1 upstream hit with redis cache",
	)
}
