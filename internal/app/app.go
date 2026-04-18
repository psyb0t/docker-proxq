package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee/serbewr"
	"github.com/psyb0t/aichteeteapee/serbewr/middleware"
	"github.com/psyb0t/common-go/cache"
	"github.com/psyb0t/ctxerrors"
	"github.com/psyb0t/proxq/internal/config"
	proxqproxy "github.com/psyb0t/proxq/internal/proxy"
	"github.com/redis/go-redis/v9"
)

func Run(configPath string) error {
	cfg, err := config.Parse(configPath)
	if err != nil {
		return ctxerrors.Wrap(err, "parse config")
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	redisOpt, client, inspector := setupAsynq(cfg)

	defer func() { _ = client.Close() }()
	defer func() { _ = inspector.Close() }()

	jobCache, cacheTTL, cacheCleanup, err := setupCache(cfg)
	if err != nil {
		return err
	}

	defer cacheCleanup()

	var wg sync.WaitGroup

	wg.Go(func() {
		runWorker(ctx, redisOpt, cfg, jobCache, cacheTTL)
	})

	err = startHTTPServer(ctx, cfg, client, inspector)

	wg.Wait()

	return err
}

func setupCache( //nolint:ireturn
	cfg config.Config,
) (cache.Cache, time.Duration, func(), error) {
	opts := cache.Options{
		Config: cache.Config{
			Mode:           cfg.Cache.Mode,
			TTL:            cfg.Cache.TTL.Std(),
			MaxEntries:     cfg.Cache.MaxEntries,
			RedisKeyPrefix: cfg.Cache.RedisKeyPrefix,
		},
		RedisClient: redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		}),
	}

	c, cleanup, err := cache.New(opts)
	if err != nil {
		return nil, 0, nil, ctxerrors.Wrap(
			err, "create cache",
		)
	}

	return c, cfg.Cache.TTL.Std(), cleanup, nil
}

func setupAsynq(
	cfg config.Config,
) (asynq.RedisClientOpt, *asynq.Client, *asynq.Inspector) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}

	return redisOpt,
		asynq.NewClient(redisOpt),
		asynq.NewInspector(redisOpt)
}

func runWorker(
	ctx context.Context,
	redisOpt asynq.RedisClientOpt,
	cfg config.Config,
	jobCache cache.Cache,
	cacheTTL time.Duration,
) {
	worker := proxqproxy.NewWorker(
		proxqproxy.WorkerConfig{
			Cache:    jobCache,
			CacheTTL: cacheTTL,
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(
		proxqproxy.TaskTypeName,
		worker.ProcessTask,
	)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: cfg.Concurrency,
		Queues: map[string]int{
			cfg.Queue: 1,
		},
		RetryDelayFunc: proxqproxy.RetryDelayFunc,
	})

	go func() {
		<-ctx.Done()
		srv.Shutdown()
	}()

	if err := srv.Run(mux); err != nil {
		slog.Error(
			"asynq worker error",
			"error", err,
		)
	}
}

func buildUpstreamConfigs(
	cfg config.Config,
) []proxqproxy.UpstreamConfig {
	upstreams := make(
		[]proxqproxy.UpstreamConfig,
		0, len(cfg.Upstreams),
	)

	for _, u := range cfg.Upstreams {
		upstreams = append(
			upstreams,
			proxqproxy.UpstreamConfig{
				Prefix:                 u.Prefix,
				URL:                    u.URL,
				Timeout:                u.Timeout.Std(),
				MaxRetries:             u.MaxRetries,
				RetryDelay:             u.RetryDelay.Std(),
				MaxBodySize:            u.MaxBodySize,
				DirectProxyThreshold:   u.DirectProxyThreshold,
				DirectProxyMode:        u.DirectProxyMode,
				CacheKeyExcludeHeaders: u.CacheKeyExcludeHeaders,
				PathFilter:             u.CompiledPatterns,
				PathFilterMode:         u.PathFilter.Mode,
			},
		)
	}

	return upstreams
}

func buildRouter(
	cfg config.Config,
	client *asynq.Client,
	inspector *asynq.Inspector,
) *serbewr.Router {
	proxyHandler := proxqproxy.NewHandler(
		client,
		proxqproxy.HandlerConfig{
			Upstreams:     buildUpstreamConfigs(cfg),
			Queue:         cfg.Queue,
			TaskRetention: cfg.TaskRetention.Std(),
		},
	)

	jobsHandler := proxqproxy.NewJobsHandler(
		inspector, cfg.Queue,
	)

	return &serbewr.Router{
		GlobalMiddlewares: []middleware.Middleware{
			middleware.RequestID(),
			middleware.Logger(),
			middleware.Recovery(),
		},
		Groups: []serbewr.GroupConfig{
			{
				Path: "/",
				Routes: buildRoutes(
					cfg, proxyHandler, jobsHandler,
				),
			},
		},
	}
}

func buildRoutes(
	cfg config.Config,
	proxyHandler *proxqproxy.Handler,
	jobsHandler *proxqproxy.JobsHandler,
) []serbewr.RouteConfig {
	jobsPath := path.Join(cfg.JobsPath, "{id}")
	contentPath := path.Join(
		cfg.JobsPath, "{id}", "content",
	)

	return []serbewr.RouteConfig{
		{
			Method:  http.MethodGet,
			Path:    contentPath,
			Handler: jobsHandler.Content,
		},
		{
			Method:  http.MethodGet,
			Path:    jobsPath,
			Handler: jobsHandler.Get,
		},
		{
			Method:  http.MethodDelete,
			Path:    jobsPath,
			Handler: jobsHandler.Cancel,
		},
		{
			Method:  "",
			Path:    "/{path...}",
			Handler: proxyHandler.ServeHTTP,
		},
	}
}

func startHTTPServer(
	ctx context.Context,
	cfg config.Config,
	client *asynq.Client,
	inspector *asynq.Inspector,
) error {
	srv, err := serbewr.NewWithConfig(serbewr.Config{
		ListenAddress: cfg.ListenAddress,
	})
	if err != nil {
		return ctxerrors.Wrap(err, "create server")
	}

	router := buildRouter(cfg, client, inspector)

	if err = srv.Start(ctx, router); err != nil {
		return ctxerrors.Wrap(err, "start server")
	}

	return nil
}
