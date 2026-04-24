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
	"github.com/psyb0t/common-go/slogging"
	"github.com/psyb0t/ctxerrors"
)

//nolint:gochecknoglobals
var defaultCacheKeyExcludeHeaders = map[string]struct{}{
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
	return &Worker{
		forwardCfg: prawxxey.ForwardConfig{
			HTTPClient: &http.Client{
				Timeout: cfg.UpstreamTimeout,
			},
			Cache:    cfg.Cache,
			CacheTTL: cfg.CacheTTL,
		},
	}
}

func (w *Worker) ProcessTask(
	ctx context.Context,
	t *asynq.Task,
) error {
	logger := slogging.GetLogger(ctx)

	var envelope taskEnvelope
	if err := json.Unmarshal(
		t.Payload(), &envelope,
	); err != nil {
		return ctxerrors.Wrap(
			err, "unmarshal task payload",
		)
	}

	logger.Debug("processing task",
		"task_id", t.Type(),
		"method", envelope.Request.Method,
		"url", envelope.Request.URL,
	)

	fwdCfg := w.forwardCfg
	fwdCfg.CacheKeyExcludeHeaders = buildExcludeHeaders(
		envelope.CacheKeyExcludeHeaders,
	)

	result, err := prawxxey.ForwardRequest(
		ctx, fwdCfg, &envelope.Request,
	)
	if err != nil {
		return ctxerrors.Wrap(
			err, "forward request",
		)
	}

	logger.Debug("task completed",
		"method", envelope.Request.Method,
		"url", envelope.Request.URL,
		"status", result.StatusCode,
	)

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

func buildExcludeHeaders(
	headers []string,
) map[string]struct{} {
	if len(headers) == 0 {
		return defaultCacheKeyExcludeHeaders
	}

	m := make(map[string]struct{}, len(headers))

	for _, h := range headers {
		m[h] = struct{}{}
	}

	return m
}
