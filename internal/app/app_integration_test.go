package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/docker-proxq/internal/config"
	"github.com/psyb0t/docker-proxq/internal/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	aclUsername = "proxq"
	// Throwaway password for the test container only.
	aclPassword       = "proxq-test-password" //nolint:gosec
	aclCacheKeyPrefix = "proxq:"
	aclTestQueue      = "default"
	aclTestTaskType   = "proxq:acl-test"
	aclCacheTTL       = time.Minute
)

// Started once in TestMain and shared by every test in the package.
var aclRedis *testinfra.Redis //nolint:gochecknoglobals

func TestMain(m *testing.M) {
	ctx := context.Background()

	redis, err := testinfra.SetupRedisWithACL(
		ctx, aclUsername, aclPassword, aclCacheKeyPrefix,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start ACL redis: %v\n", err)
		os.Exit(1)
	}

	aclRedis = redis

	code := m.Run()

	redis.Teardown(ctx)
	os.Exit(code)
}

func writeACLConfig(t *testing.T, password string) config.Config {
	t.Helper()

	content := fmt.Sprintf(`
redis:
  addr: %q
  username: %q
  password: %q
queue: %q
cache:
  mode: "redis"
  redisKeyPrefix: %q
upstreams:
  - prefix: "/"
    url: "http://upstream:3000"
`, aclRedis.Addr, aclUsername, password, aclTestQueue, aclCacheKeyPrefix)

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.Parse(path)
	require.NoError(t, err)

	return cfg
}

func TestSetupAsynq_RedisACLUser(t *testing.T) {
	cfg := writeACLConfig(t, aclPassword)

	_, client, inspector := setupAsynq(cfg)

	t.Cleanup(func() {
		assert.NoError(t, client.Close())
		assert.NoError(t, inspector.Close())
	})

	info, err := client.Enqueue(
		asynq.NewTask(aclTestTaskType, []byte("payload")),
		asynq.Queue(cfg.Queue),
	)
	require.NoError(t, err)

	task, err := inspector.GetTaskInfo(cfg.Queue, info.ID)
	require.NoError(t, err)
	assert.Equal(t, aclTestTaskType, task.Type)
}

func TestSetupCache_RedisACLUser(t *testing.T) {
	cfg := writeACLConfig(t, aclPassword)

	jobCache, _, cleanup, err := setupCache(cfg)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(
		t, jobCache.Set(ctx, "acl-key", []byte("cached"), aclCacheTTL),
	)

	got, err := jobCache.Get(ctx, "acl-key")
	require.NoError(t, err)
	assert.Equal(t, []byte("cached"), got)
}

func TestSetupAsynq_RedisACLWrongPasswordFails(t *testing.T) {
	cfg := writeACLConfig(t, "wrong-password")

	_, client, inspector := setupAsynq(cfg)

	t.Cleanup(func() {
		assert.NoError(t, client.Close())
		assert.NoError(t, inspector.Close())
	})

	_, err := client.Enqueue(
		asynq.NewTask(aclTestTaskType, nil),
		asynq.Queue(cfg.Queue),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WRONGPASS")
}
