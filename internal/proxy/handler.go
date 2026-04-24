package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee"
	proxylib "github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	"github.com/psyb0t/common-go/slogging"
	"github.com/psyb0t/ctxerrors"
	"github.com/psyb0t/docker-proxq/internal/config"
)

const defaultTaskRetention = 1 * time.Hour

type UpstreamConfig struct {
	Prefix                 string
	URL                    string
	Timeout                time.Duration
	MaxRetries             int
	RetryDelay             time.Duration
	MaxBodySize            int64
	DirectProxyThreshold   int64
	DirectProxyMode        string
	CacheKeyExcludeHeaders []string
	PathFilter             []*regexp.Regexp
	PathFilterMode         string
}

type upstream struct {
	prefix                 string
	url                    string
	timeout                time.Duration
	maxRetries             int
	retryDelay             time.Duration
	maxBodySize            int64
	directProxyThreshold   int64
	directProxyMode        string
	cacheKeyExcludeHeaders []string
	pathFilter             []*regexp.Regexp
	pathFilterMode         string
	reverseProxy           *httputil.ReverseProxy
}

type HandlerConfig struct {
	Upstreams     []UpstreamConfig
	Queue         string
	TaskRetention time.Duration
}

type Handler struct {
	client        *asynq.Client
	queue         string
	taskRetention time.Duration
	upstreams     []upstream
}

func NewHandler(
	client *asynq.Client,
	cfg HandlerConfig,
) *Handler {
	queue := cfg.Queue
	if queue == "" {
		queue = DefaultQueue
	}

	retention := cfg.TaskRetention
	if retention == 0 {
		retention = defaultTaskRetention
	}

	upstreams := make(
		[]upstream, 0, len(cfg.Upstreams),
	)

	for _, uc := range cfg.Upstreams {
		upstreams = append(upstreams, upstream{
			prefix:                 uc.Prefix,
			url:                    strings.TrimRight(uc.URL, "/"),
			timeout:                uc.Timeout,
			maxRetries:             uc.MaxRetries,
			retryDelay:             uc.RetryDelay,
			maxBodySize:            uc.MaxBodySize,
			directProxyThreshold:   uc.DirectProxyThreshold,
			directProxyMode:        uc.DirectProxyMode,
			cacheKeyExcludeHeaders: uc.CacheKeyExcludeHeaders,
			pathFilter:             uc.PathFilter,
			pathFilterMode:         uc.PathFilterMode,
			reverseProxy:           buildReverseProxy(uc.URL),
		})
	}

	return &Handler{
		client:        client,
		queue:         queue,
		taskRetention: retention,
		upstreams:     upstreams,
	}
}

func buildReverseProxy(
	rawURL string,
) *httputil.ReverseProxy {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}

	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = r.In.Host
		},
		ErrorHandler: func(
			w http.ResponseWriter,
			r *http.Request,
			err error,
		) {
			logger := slogging.GetLogger(
				r.Context(),
			)
			logger.Error(
				"reverse proxy error",
				"error", err,
			)
			setProxqSourceHeader(w)
			proxylib.WriteError(
				w, http.StatusBadGateway,
			)
		},
	}
}

type acceptedResponse struct {
	JobID string `json:"jobId"`
}

func (h *Handler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	u, strippedURI := h.resolveUpstream(r)
	if u == nil {
		logger := slogging.GetLogger(r.Context())
		logger.Warn(
			"no upstream match",
			"path", r.URL.Path,
		)

		setProxqSourceHeader(w)
		proxylib.WriteError(
			w, http.StatusBadGateway,
		)

		return
	}

	if isWebSocketUpgrade(r) {
		h.proxyWebSocket(w, r, u)

		return
	}

	if shouldBypassQueue(r, u) {
		h.directProxy(w, r, u, strippedURI)

		return
	}

	h.enqueueRequest(w, r, u, strippedURI)
}

func (h *Handler) resolveUpstream(
	r *http.Request,
) (*upstream, string) {
	path := r.URL.Path

	for i := range h.upstreams {
		u := &h.upstreams[i]

		if u.prefix == "/" {
			return u, r.RequestURI
		}

		if path == u.prefix ||
			strings.HasPrefix(path, u.prefix+"/") {
			stripped := strings.TrimPrefix(
				r.RequestURI, u.prefix,
			)
			if stripped == "" {
				stripped = "/"
			}

			return u, stripped
		}
	}

	return nil, ""
}

func isChunkedTransfer(r *http.Request) bool {
	for _, te := range r.TransferEncoding {
		if strings.EqualFold(te, "chunked") {
			return true
		}
	}

	return false
}

func shouldBypassQueue(
	r *http.Request,
	u *upstream,
) bool {
	if isChunkedTransfer(r) {
		return true
	}

	if u.directProxyThreshold > 0 &&
		r.ContentLength > u.directProxyThreshold {
		return true
	}

	return pathFilterBypasses(r.URL.Path, u)
}

func matchesPathFilter(
	path string,
	patterns []*regexp.Regexp,
) bool {
	for _, re := range patterns {
		if re.MatchString(path) {
			return true
		}
	}

	return false
}

func pathFilterBypasses(
	path string,
	u *upstream,
) bool {
	if len(u.pathFilter) == 0 {
		return false
	}

	matches := matchesPathFilter(path, u.pathFilter)

	switch u.pathFilterMode {
	case config.PathFilterModeWhitelist:
		return !matches
	default:
		return matches
	}
}

func (h *Handler) directProxy(
	w http.ResponseWriter,
	r *http.Request,
	u *upstream,
	strippedURI string,
) {
	logger := slogging.GetLogger(r.Context())

	if u.directProxyMode == config.DirectProxyModeRedirect {
		target := u.url + strippedURI

		logger.Debug("redirecting to upstream",
			"target", target,
		)

		setProxqSourceHeader(w)
		http.Redirect(
			w, r, target,
			http.StatusTemporaryRedirect,
		)

		return
	}

	if u.reverseProxy == nil {
		logger.Error("reverse proxy not configured")
		setProxqSourceHeader(w)
		proxylib.WriteError(
			w, http.StatusBadGateway,
		)

		return
	}

	logger.Debug("direct proxying request",
		"upstream", u.prefix,
		"content_length", r.ContentLength,
	)

	r.URL.Path = stripPrefix(r.URL.Path, u.prefix)
	u.reverseProxy.ServeHTTP(w, r)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(
		r.Header.Get(
			aichteeteapee.HeaderNameConnection,
		),
		"upgrade",
	) && strings.EqualFold(
		r.Header.Get(
			aichteeteapee.HeaderNameUpgrade,
		),
		"websocket",
	)
}

func (h *Handler) proxyWebSocket(
	w http.ResponseWriter,
	r *http.Request,
	u *upstream,
) {
	if u.reverseProxy == nil {
		logger := slogging.GetLogger(r.Context())
		logger.Error(
			"websocket proxy not configured",
		)
		setProxqSourceHeader(w)
		proxylib.WriteError(
			w, http.StatusBadGateway,
		)

		return
	}

	logger := slogging.GetLogger(r.Context())
	logger.Debug("proxying websocket connection",
		"upstream", u.prefix,
	)

	r.URL.Path = stripPrefix(r.URL.Path, u.prefix)
	u.reverseProxy.ServeHTTP(w, r)
}

func parseRequestTimeout(
	r *http.Request,
) (time.Duration, error) {
	val := r.Header.Get(HeaderNameXProxqTimeout)
	if val == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, ctxerrors.Wrap(err, "parse duration")
	}

	return d, nil
}

func resolveTimeout(
	r *http.Request,
	u *upstream,
) (time.Duration, error) {
	reqTimeout, err := parseRequestTimeout(r)
	if err != nil {
		return 0, err
	}

	if reqTimeout > 0 {
		return reqTimeout, nil
	}

	return u.timeout, nil
}

func readBody(
	w http.ResponseWriter,
	r *http.Request,
	maxBodySize int64,
) ([]byte, bool) {
	body, err := io.ReadAll(
		io.LimitReader(r.Body, maxBodySize),
	)
	if err != nil {
		logger := slogging.GetLogger(r.Context())
		logger.Error(
			"failed to read request body",
			"error", err,
		)
		setProxqSourceHeader(w)
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return nil, false
	}

	return body, true
}

func (h *Handler) enqueueRequest(
	w http.ResponseWriter,
	r *http.Request,
	u *upstream,
	strippedURI string,
) {
	logger := slogging.GetLogger(r.Context())

	body, ok := readBody(w, r, u.maxBodySize)
	if !ok {
		return
	}

	timeout, err := resolveTimeout(r, u)
	if err != nil {
		logger.Warn(
			"invalid X-Proxq-Timeout header",
			"value", r.Header.Get(HeaderNameXProxqTimeout),
			"error", err,
		)
		setProxqSourceHeader(w)
		proxylib.WriteError(w, http.StatusBadRequest)

		return
	}

	envelope := buildEnvelope(r, u, strippedURI, body)

	taskID, err := h.enqueue(
		r, envelope, timeout, u.maxRetries,
	)
	if err != nil {
		logger.Error(
			"failed to enqueue task",
			"error", err,
		)
		setProxqSourceHeader(w)
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return
	}

	logger.Debug("request enqueued",
		"job_id", taskID,
		"upstream", u.prefix,
		"method", r.Method,
		"url", u.url+strippedURI,
	)

	setProxqSourceHeader(w)

	aichteeteapee.WriteJSON(
		w,
		http.StatusAccepted,
		acceptedResponse{JobID: taskID},
	)
}

func buildEnvelope(
	r *http.Request,
	u *upstream,
	strippedURI string,
	body []byte,
) taskEnvelope {
	return taskEnvelope{
		Request: proxylib.RequestPayload{
			Method:   r.Method,
			URL:      u.url + strippedURI,
			Headers:  r.Header,
			Body:     body,
			ClientIP: aichteeteapee.GetClientIP(r),
			Proto:    proxylib.RequestScheme(r),
		},
		RetryDelay:             u.retryDelay,
		CacheKeyExcludeHeaders: u.cacheKeyExcludeHeaders,
	}
}

func (h *Handler) enqueue(
	r *http.Request,
	envelope taskEnvelope,
	timeout time.Duration,
	maxRetries int,
) (string, error) {
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", ctxerrors.Wrap(
			err, "marshal payload",
		)
	}

	taskID := uuid.New().String()
	task := asynq.NewTask(TaskTypeName, data)

	opts := []asynq.Option{
		asynq.TaskID(taskID),
		asynq.Queue(h.queue),
		asynq.MaxRetry(maxRetries),
		asynq.Retention(h.taskRetention),
	}

	if timeout > 0 {
		opts = append(opts, asynq.Timeout(timeout))
	}

	_, err = h.client.EnqueueContext(
		r.Context(),
		task,
		opts...,
	)
	if err != nil {
		return "", ctxerrors.Wrap(
			err, "enqueue task",
		)
	}

	return taskID, nil
}

func stripPrefix(path, prefix string) string {
	if prefix == "/" {
		return path
	}

	stripped := strings.TrimPrefix(path, prefix)
	if stripped == "" {
		return "/"
	}

	return stripped
}
