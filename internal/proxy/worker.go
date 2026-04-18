package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee"
	"github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	"github.com/psyb0t/common-go/cache"
	"github.com/psyb0t/ctxerrors"
)

const defaultUpstreamTimeout = 5 * time.Minute

//nolint:gochecknoglobals
var cacheKeyExcludeHeaders = map[string]struct{}{
	aichteeteapee.HeaderNameXRequestID:      {},
	aichteeteapee.HeaderNameXForwardedFor:   {},
	aichteeteapee.HeaderNameXRealIP:         {},
	aichteeteapee.HeaderNameXForwardedProto: {},
}

type WorkerConfig struct {
	UpstreamTimeout time.Duration
	Cache           cache.Cache
	CacheTTL        time.Duration
}

type Worker struct {
	forwardCfg prawxxey.ForwardConfig
}

func NewWorker(cfg WorkerConfig) *Worker {
	timeout := cfg.UpstreamTimeout
	if timeout == 0 {
		timeout = defaultUpstreamTimeout
	}

	return &Worker{
		forwardCfg: prawxxey.ForwardConfig{
			HTTPClient: &http.Client{
				Timeout: timeout,
			},
			Cache:                  cfg.Cache,
			CacheTTL:               cfg.CacheTTL,
			CacheKeyExcludeHeaders: cacheKeyExcludeHeaders,
		},
	}
}

func (w *Worker) ProcessTask(
	ctx context.Context,
	t *asynq.Task,
) error {
	var envelope taskEnvelope
	if err := json.Unmarshal(
		t.Payload(), &envelope,
	); err != nil {
		return ctxerrors.Wrap(
			err, "unmarshal task payload",
		)
	}

	result, err := prawxxey.ForwardRequest(
		ctx, w.forwardCfg, &envelope.Request,
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
