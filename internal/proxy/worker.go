package proxy

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	"github.com/psyb0t/common-go/cache"
	"github.com/psyb0t/ctxerrors"
)

const defaultUpstreamTimeout = 5 * time.Minute

type WorkerConfig struct {
	UpstreamTimeout time.Duration
	Cache           cache.Cache
	CacheTTL        time.Duration
	Logger          *slog.Logger
}

type Worker struct {
	forwardCfg prawxxey.ForwardConfig
	logger     *slog.Logger
}

func NewWorker(cfg WorkerConfig) *Worker {
	timeout := cfg.UpstreamTimeout
	if timeout == 0 {
		timeout = defaultUpstreamTimeout
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Worker{
		forwardCfg: prawxxey.ForwardConfig{
			HTTPClient: &http.Client{
				Timeout: timeout,
			},
			Cache:    cfg.Cache,
			CacheTTL: cfg.CacheTTL,
		},
		logger: logger,
	}
}

func (w *Worker) ProcessTask(
	ctx context.Context,
	t *asynq.Task,
) error {
	var payload prawxxey.RequestPayload
	if err := json.Unmarshal(
		t.Payload(), &payload,
	); err != nil {
		return ctxerrors.Wrap(
			err, "unmarshal task payload",
		)
	}

	result, err := prawxxey.ForwardRequest(
		ctx, w.forwardCfg, &payload,
	)
	if err != nil {
		return ctxerrors.Wrap(
			err, "forward request",
		)
	}

	resultData, err := json.Marshal(result)
	if err != nil {
		return ctxerrors.Wrap(
			err, "marshal result",
		)
	}

	if _, err := t.ResultWriter().Write(
		resultData,
	); err != nil {
		return ctxerrors.Wrap(
			err, "write result",
		)
	}

	return nil
}
