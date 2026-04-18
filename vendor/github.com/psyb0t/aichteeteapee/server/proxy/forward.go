package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/psyb0t/aichteeteapee"
	"github.com/psyb0t/common-go/cache"
	"github.com/psyb0t/ctxerrors"
)

const defaultCacheTTL = 5 * time.Minute

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
	HTTPClient *http.Client
	Cache      cache.Cache
	CacheTTL   time.Duration
	CacheKeyFn func(p *RequestPayload) string
}

func ForwardRequest(
	ctx context.Context,
	cfg ForwardConfig,
	payload *RequestPayload,
) (*ResponseResult, error) {
	if cfg.Cache != nil {
		return forwardWithCache(ctx, cfg, payload)
	}

	return doUpstreamRequest(ctx, cfg.HTTPClient, payload)
}

func forwardWithCache(
	ctx context.Context,
	cfg ForwardConfig,
	payload *RequestPayload,
) (*ResponseResult, error) {
	key := cacheKey(cfg, payload)

	data, err := cfg.Cache.Get(ctx, key)
	if err == nil {
		var result ResponseResult
		if jsonErr := json.Unmarshal(
			data, &result,
		); jsonErr == nil {
			return &result, nil
		}
	}

	result, err := doUpstreamRequest(
		ctx, cfg.HTTPClient, payload,
	)
	if err != nil {
		return nil, err
	}

	if result.StatusCode >= http.StatusOK &&
		result.StatusCode < http.StatusMultipleChoices {
		ttl := cfg.CacheTTL
		if ttl == 0 {
			ttl = defaultCacheTTL
		}

		if encoded, jsonErr := json.Marshal(
			result,
		); jsonErr == nil {
			_ = cfg.Cache.Set(ctx, key, encoded, ttl)
		}
	}

	return result, nil
}

func doUpstreamRequest(
	ctx context.Context,
	httpClient *http.Client,
	payload *RequestPayload,
) (*ResponseResult, error) {
	var bodyReader io.Reader
	if len(payload.Body) > 0 {
		bodyReader = bytes.NewReader(payload.Body)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		payload.Method,
		payload.URL,
		bodyReader,
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

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "do request",
		)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ctxerrors.Wrap(
			err, "read response body",
		)
	}

	return &ResponseResult{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, nil
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

	return payload.Hash()
}
