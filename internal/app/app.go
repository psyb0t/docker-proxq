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

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	redisOpt, client, inspector := setupAsynq(cfg)

	defer func() { _ = client.Close() }()
	defer func() { _ = inspector.Close() }()

	cacheCfg, err := cache.ParseConfig()
	if err != nil {
		return ctxerrors.Wrap(
			err, "parse cache config",
		)
	}

	jobCache, cleanup, err := cache.FromConfig(cacheCfg)
	if err != nil {
		return ctxerrors.Wrap(
			err, "create cache from config",
		)
	}

	defer cleanup()

	var wg sync.WaitGroup

	wg.Go(func() {
		runWorker(
			ctx, redisOpt, cfg,
			jobCache, cacheCfg.TTL,
		)
	})

	err = startHTTPServer(
		ctx, cfg, client, inspector,
	)

	wg.Wait()

	return err
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
) *serbewr.Router {
	proxyHandler := proxqproxy.NewHandler(
		client,
		proxqproxy.HandlerConfig{
			UpstreamURL:        cfg.UpstreamURL,
			MaxRequestBodySize: cfg.MaxBodySize,
			Queue:              cfg.Queue,
			TaskRetention:      cfg.TaskRetention,
		},
	)

	jobsHandler := proxqproxy.NewJobsHandler(
		inspector, cfg.Queue, slog.Default(),
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
) error {
	srv, err := serbewr.New()
	if err != nil {
		return ctxerrors.Wrap(err, "create server")
	}

	router := buildRouter(cfg, client, inspector)

	if err = srv.Start(ctx, router); err != nil {
		return ctxerrors.Wrap(err, "start server")
	}

	return nil
}
