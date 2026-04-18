package prawxxey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/psyb0t/common-go/cache"
	"github.com/psyb0t/common-go/slogging"
	"github.com/psyb0t/ctxerrors"
)

var errResponseBodyTooLarge = errors.New(
	"upstream response body exceeds maximum size",
)

const (
	defaultCacheTTL            = 5 * time.Minute
	defaultMaxResponseBodySize = 100 << 20 // 100MB
)

// hop-by-hop headers (RFC 2616 section 13.5.1).
//
//nolint:gochecknoglobals
var hopByHopHeaders = map[string]struct{}{
	aichteeteapee.HeaderNameConnection:         {},
	aichteeteapee.HeaderNameKeepAlive:          {},
	aichteeteapee.HeaderNameProxyAuthenticate:  {},
	aichteeteapee.HeaderNameProxyAuthorization: {},
	aichteeteapee.HeaderNameTE:                 {},
	aichteeteapee.HeaderNameTrailers:           {},
	aichteeteapee.HeaderNameTransferEncoding:   {},
	aichteeteapee.HeaderNameUpgrade:            {},
}

type ForwardConfig struct {
	HTTPClient             *http.Client
	Cache                  cache.Cache
	CacheTTL               time.Duration
	CacheKeyFn             func(p *RequestPayload) string
	CacheKeyExcludeHeaders map[string]struct{}
	MaxResponseBodySize    int64
}

func ForwardRequest(
	ctx context.Context,
	cfg ForwardConfig,
	payload *RequestPayload,
) (*ResponseResult, error) {
	logger := slogging.GetLogger(ctx)

	logger.Debug(
		"forwarding request",
		"upstreamMethod", payload.Method,
		"upstreamURL", payload.URL,
	)

	if cfg.Cache != nil {
		return forwardWithCache(ctx, cfg, payload)
	}

	maxSize := cfg.MaxResponseBodySize
	if maxSize <= 0 {
		maxSize = defaultMaxResponseBodySize
	}

	return doUpstreamRequest(ctx, cfg.HTTPClient, payload, maxSize)
}

func forwardWithCache(
	ctx context.Context,
	cfg ForwardConfig,
	payload *RequestPayload,
) (*ResponseResult, error) {
	key := cacheKey(cfg, payload)

	if result, ok := tryCache(ctx, cfg, key); ok {
		return result, nil
	}

	maxSize := cfg.MaxResponseBodySize
	if maxSize <= 0 {
		maxSize = defaultMaxResponseBodySize
	}

	result, err := doUpstreamRequest(
		ctx, cfg.HTTPClient, payload, maxSize,
	)
	if err != nil {
		return nil, err
	}

	storeInCache(ctx, cfg, key, result)

	if result.Headers == nil {
		result.Headers = make(map[string][]string)
	}

	result.Headers[aichteeteapee.HeaderNameXCacheStatus] = []string{
		aichteeteapee.CacheStatusMiss,
	}

	return result, nil
}

func tryCache(
	ctx context.Context,
	cfg ForwardConfig,
	key string,
) (*ResponseResult, bool) {
	logger := slogging.GetLogger(ctx)

	data, err := cfg.Cache.Get(ctx, key)
	if err != nil {
		return nil, false
	}

	var result ResponseResult

	if jsonErr := json.Unmarshal(
		data, &result,
	); jsonErr != nil {
		logger.Warn(
			"cache entry corrupt",
			"cacheKey", key,
			"error", jsonErr,
		)

		return nil, false
	}

	logger.Debug(
		"cache hit",
		"cacheKey", key,
		"statusCode", result.StatusCode,
	)

	if result.Headers == nil {
		result.Headers = make(map[string][]string)
	}

	result.Headers[aichteeteapee.HeaderNameXCacheStatus] = []string{
		aichteeteapee.CacheStatusHit,
	}

	return &result, true
}

func storeInCache(
	ctx context.Context,
	cfg ForwardConfig,
	key string,
	result *ResponseResult,
) {
	logger := slogging.GetLogger(ctx)

	if result.StatusCode < http.StatusOK ||
		result.StatusCode >= http.StatusMultipleChoices {
		logger.Debug(
			"not caching non-2xx response",
			"statusCode", result.StatusCode,
		)

		return
	}

	ttl := cfg.CacheTTL
	if ttl == 0 {
		ttl = defaultCacheTTL
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return
	}

	_ = cfg.Cache.Set(ctx, key, encoded, ttl)

	logger.Debug(
		"cached response",
		"cacheKey", key,
		"ttl", ttl.String(),
	)
}

func doUpstreamRequest(
	ctx context.Context,
	httpClient *http.Client,
	payload *RequestPayload,
	maxResponseBodySize int64,
) (*ResponseResult, error) {
	start := time.Now()

	req, err := buildUpstreamReq(ctx, payload)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		slogging.GetLogger(ctx).Error(
			"upstream request failed",
			"upstreamURL", payload.URL,
			"duration", time.Since(start).String(),
			"error", err,
		)

		return nil, ctxerrors.Wrap(
			err, "do request",
		)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(
		io.LimitReader(resp.Body, maxResponseBodySize+1),
	)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "read response body",
		)
	}

	if int64(len(respBody)) > maxResponseBodySize {
		return nil, ctxerrors.Wrap(
			errResponseBodyTooLarge,
			"read response body",
		)
	}

	logUpstreamResponse(
		ctx, payload.URL,
		resp.StatusCode, len(respBody),
		time.Since(start),
	)

	return &ResponseResult{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, nil
}

func buildUpstreamReq(
	ctx context.Context,
	payload *RequestPayload,
) (*http.Request, error) {
	var bodyReader io.Reader
	if len(payload.Body) > 0 {
		bodyReader = bytes.NewReader(payload.Body)
	}

	req, err := http.NewRequestWithContext(
		ctx, payload.Method,
		payload.URL, bodyReader,
	)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "create request",
		)
	}

	if len(payload.Body) > 0 {
		req.ContentLength = int64(
			len(payload.Body),
		)
	}

	setUpstreamHeaders(req, payload)

	return req, nil
}

func logUpstreamResponse(
	ctx context.Context,
	url string,
	statusCode, bodySize int,
	duration time.Duration,
) {
	logger := slogging.GetLogger(ctx).With(
		"upstreamURL", url,
		"statusCode", statusCode,
		"duration", duration.String(),
		"bodySize", bodySize,
	)

	if statusCode >= http.StatusInternalServerError {
		logger.Warn("upstream returned server error")

		return
	}

	logger.Debug("upstream response received")
}

func setUpstreamHeaders(
	req *http.Request,
	payload *RequestPayload,
) {
	for key, vals := range payload.Headers {
		canonKey := http.CanonicalHeaderKey(key)
		if _, skip := hopByHopHeaders[canonKey]; skip {
			continue
		}

		for _, v := range vals {
			req.Header.Add(key, v)
		}
	}

	if payload.ClientIP != "" {
		req.Header.Set(
			aichteeteapee.HeaderNameXForwardedFor,
			payload.ClientIP,
		)
		req.Header.Set(
			aichteeteapee.HeaderNameXRealIP,
			payload.ClientIP,
		)
	}

	if payload.Proto != "" {
		req.Header.Set(
			aichteeteapee.HeaderNameXForwardedProto,
			payload.Proto,
		)
	}
}

func RequestScheme(r *http.Request) string {
	if r.TLS != nil {
		return aichteeteapee.SchemeHTTPS
	}

	if proto := r.Header.Get(
		aichteeteapee.HeaderNameXForwardedProto,
	); proto != "" {
		return proto
	}

	return aichteeteapee.SchemeHTTP
}

func WriteError(
	w http.ResponseWriter,
	status int,
) {
	aichteeteapee.WriteJSON(
		w,
		status,
		aichteeteapee.ErrorResponse{
			Code: aichteeteapee.ErrorCodeFromHTTPStatus(
				status,
			),
			Message: http.StatusText(status),
		},
	)
}

func cacheKey(
	cfg ForwardConfig,
	payload *RequestPayload,
) string {
	if cfg.CacheKeyFn != nil {
		return cfg.CacheKeyFn(payload)
	}

	return payload.HashExcluding(
		cfg.CacheKeyExcludeHeaders,
	)
}
