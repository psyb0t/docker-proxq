package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const proxqImage = "proxq:e2e-test"

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

type jobStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

type env struct {
	proxqURL  string
	countsURL string
	cleanup   func()
}

type setupOpts struct {
	cacheMode    string
	extraConfig  string
	upstreamsCfg string
}

func defaultUpstreamsCfg() string {
	return `
upstreams:
  - prefix: "/"
    url: "http://upstream:3000"
    timeout: "5m"
`
}

func buildConfigYAML(opts setupOpts) string {
	upstreams := opts.upstreamsCfg
	if upstreams == "" {
		upstreams = defaultUpstreamsCfg()
	}

	cacheMode := opts.cacheMode
	if cacheMode == "" {
		cacheMode = "none"
	}

	cfg := fmt.Sprintf(`
listenAddress: "0.0.0.0:8080"
redis:
  addr: "redis:6379"
queue: "default"
concurrency: 5
jobsPath: "/__jobs"
taskRetention: "1h"
cache:
  mode: "%s"
  ttl: "30s"
  maxEntries: 1000
  redisKeyPrefix: "proxq:"
%s
%s
`, cacheMode, opts.extraConfig, upstreams)

	return cfg
}

func setup(
	t *testing.T,
	opts setupOpts,
) env {
	t.Helper()

	ctx := context.Background()

	net, err := network.New(ctx)
	require.NoError(t, err)

	redisCtr, err := redis.Run(
		ctx, "redis:7-alpine",
		network.WithNetwork(
			[]string{"redis"}, net,
		),
	)
	require.NoError(t, err)

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

	host, err := upstream.Host(ctx)
	require.NoError(t, err)

	port, err := upstream.MappedPort(ctx, "3000")
	require.NoError(t, err)

	countsURL := fmt.Sprintf(
		"http://%s:%s", host, port.Port(),
	)

	configContent := buildConfigYAML(opts)
	configPath := writeConfigFile(t, configContent)

	proxq, err := testcontainers.GenericContainer(
		ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        proxqImage,
				ExposedPorts: []string{"8080/tcp"},
				Networks:     []string{net.Name},
				Env: map[string]string{
					"PROXQ_CONFIG": "/app/config.yaml",
				},
				Files: []testcontainers.ContainerFile{
					{
						HostFilePath:      configPath,
						ContainerFilePath: "/app/config.yaml",
						FileMode:          0o644,
					},
				},
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

	proxqPort, err := proxq.MappedPort(
		ctx, "8080",
	)
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
			_ = redisCtr.Terminate(ctx)
			_ = net.Remove(ctx)
		},
	}
}

func writeConfigFile(
	t *testing.T, content string,
) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, os.WriteFile(
		path, []byte(content), 0o600,
	))

	return path
}

func submitJob(
	t *testing.T,
	proxqURL, method, path string,
	body io.Reader,
) string {
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

	require.Equal(
		t, http.StatusAccepted, resp.StatusCode,
	)

	var jr jobResponse

	require.NoError(t, json.NewDecoder(
		resp.Body,
	).Decode(&jr))
	require.NotEmpty(t, jr.JobID)

	return jr.JobID
}

func pollStatus(
	t *testing.T,
	proxqURL, jobID, jobsPath string,
	timeout time.Duration,
) jobStatus {
	t.Helper()

	if jobsPath == "" {
		jobsPath = "/__jobs"
	}

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(
			proxqURL + jobsPath + "/" + jobID,
		)
		if err != nil {
			time.Sleep(200 * time.Millisecond)

			continue
		}

		var info jobStatus

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

	return jobStatus{}
}

func getContent(
	t *testing.T,
	proxqURL, jobID, jobsPath string,
) *http.Response {
	t.Helper()

	if jobsPath == "" {
		jobsPath = "/__jobs"
	}

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(
		proxqURL + jobsPath + "/" + jobID + "/content",
	)
	require.NoError(t, err)

	return resp
}

func getRequestCounts(
	t *testing.T,
	countsURL string,
) map[string]int {
	t.Helper()

	resp, err := http.Get(
		countsURL + "/request-counts",
	)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	var counts map[string]int

	require.NoError(t, json.NewDecoder(
		resp.Body,
	).Decode(&counts))

	return counts
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
