package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
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

func Run() error {
	cfg, err := config.Parse()
	if err != nil {
		return ctxerrors.Wrap(err, "parse config")
	}

	if cfg.UpstreamURL == "" {
		slog.Error("PROXQ_UPSTREAM_URL is required")
		os.Exit(1)
	}

	directProxyPaths, err := parseDirectProxyPaths(
		cfg.DirectProxyPaths,
	)
	if err != nil {
		return ctxerrors.Wrap(
			err, "parse direct proxy paths",
		)
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
		runWorker(
			ctx, redisOpt, cfg,
			jobCache, cacheTTL,
		)
	})

	err = startHTTPServer(
		ctx, cfg, client, inspector,
		directProxyPaths,
	)

	wg.Wait()

	return err
}

const cacheRedisKeyPrefix = "proxq:"

func setupCache( //nolint:ireturn
	cfg config.Config,
) (cache.Cache, time.Duration, func(), error) {
	cacheCfg, err := cache.ParseConfig()
	if err != nil {
		return nil, 0, nil, ctxerrors.Wrap(
			err, "parse cache config",
		)
	}

	cacheCfg.RedisKeyPrefix = cacheRedisKeyPrefix
	cacheCfg.RedisClient = redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	c, cleanup, err := cache.New(cacheCfg)
	if err != nil {
		return nil, 0, nil, ctxerrors.Wrap(
			err, "create cache from config",
		)
	}

	return c, cacheCfg.TTL, cleanup, nil
}

func setupAsynq(
	cfg config.Config,
) (asynq.RedisClientOpt, *asynq.Client, *asynq.Inspector) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
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
			UpstreamTimeout: cfg.UpstreamTimeout,
			Cache:           jobCache,
			CacheTTL:        cacheTTL,
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

func buildRouter(
	cfg config.Config,
	client *asynq.Client,
	inspector *asynq.Inspector,
	directProxyPaths []*regexp.Regexp,
) *serbewr.Router {
	proxyHandler := proxqproxy.NewHandler(
		client,
		proxqproxy.HandlerConfig{
			UpstreamURL:          cfg.UpstreamURL,
			MaxRequestBodySize:   cfg.MaxBodySize,
			DirectProxyThreshold: cfg.DirectProxyThreshold,
			DirectProxyPaths:     directProxyPaths,
			Queue:                cfg.Queue,
			TaskRetention:        cfg.TaskRetention,
		},
	)

	jobsHandler := proxqproxy.NewJobsHandler(
		inspector, cfg.Queue,
	)

	jobsPath := path.Join(cfg.JobsPath, "{id}")

	return &serbewr.Router{
		GlobalMiddlewares: []middleware.Middleware{
			middleware.RequestID(),
			middleware.Logger(),
			middleware.Recovery(),
		},
		Groups: []serbewr.GroupConfig{
			{
				Path: "/",
				Routes: []serbewr.RouteConfig{
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
				},
			},
		},
	}
}

func startHTTPServer(
	ctx context.Context,
	cfg config.Config,
	client *asynq.Client,
	inspector *asynq.Inspector,
	directProxyPaths []*regexp.Regexp,
) error {
	srv, err := serbewr.NewWithConfig(serbewr.Config{
		ListenAddress: cfg.ListenAddress,
	})
	if err != nil {
		return ctxerrors.Wrap(err, "create server")
	}

	router := buildRouter(
		cfg, client, inspector, directProxyPaths,
	)

	if err = srv.Start(ctx, router); err != nil {
		return ctxerrors.Wrap(err, "start server")
	}

	return nil
}

func parseDirectProxyPaths(
	raw string,
) ([]*regexp.Regexp, error) {
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	patterns := make([]*regexp.Regexp, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		re, err := regexp.Compile(p)
		if err != nil {
			return nil, ctxerrors.Wrap(
				err, "compile regex: "+p,
			)
		}

		patterns = append(patterns, re)
	}

	return patterns, nil
}
