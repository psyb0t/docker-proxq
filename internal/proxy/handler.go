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
)

const (
	defaultMaxRequestBodySize = 10 << 20 // 10MB
	defaultTaskRetention      = 1 * time.Hour
)

type DirectProxyMode = string

const (
	DirectProxyModeProxy    DirectProxyMode = "proxy"
	DirectProxyModeRedirect DirectProxyMode = "redirect"
)

type HandlerConfig struct {
	UpstreamURL          string
	MaxRequestBodySize   int64
	DirectProxyThreshold int64
	DirectProxyPaths     []*regexp.Regexp
	DirectProxyMode      string
	Queue                string
	TaskRetention        time.Duration
}

type Handler struct {
	client               *asynq.Client
	upstreamURL          string
	queue                string
	taskRetention        time.Duration
	maxRequestBodySize   int64
	directProxyThreshold int64
	directProxyPaths     []*regexp.Regexp
	directProxyMode      string
	reverseProxy         *httputil.ReverseProxy
}

func NewHandler(
	client *asynq.Client,
	cfg HandlerConfig,
) *Handler {
	maxBody := cfg.MaxRequestBodySize
	if maxBody == 0 {
		maxBody = defaultMaxRequestBodySize
	}

	queue := cfg.Queue
	if queue == "" {
		queue = DefaultQueue
	}

	retention := cfg.TaskRetention
	if retention == 0 {
		retention = defaultTaskRetention
	}

	proxyMode := cfg.DirectProxyMode
	if proxyMode == "" {
		proxyMode = DirectProxyModeProxy
	}

	h := &Handler{
		client:               client,
		upstreamURL:          cfg.UpstreamURL,
		queue:                queue,
		taskRetention:        retention,
		maxRequestBodySize:   maxBody,
		directProxyThreshold: cfg.DirectProxyThreshold,
		directProxyPaths:     cfg.DirectProxyPaths,
		directProxyMode:      proxyMode,
	}

	h.reverseProxy = buildReverseProxy(cfg.UpstreamURL)

	return h
}

func buildReverseProxy(
	upstream string,
) *httputil.ReverseProxy {
	target, err := url.Parse(upstream)
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
				"websocket proxy error",
				"error", err,
			)
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
	if isWebSocketUpgrade(r) {
		h.proxyWebSocket(w, r)

		return
	}

	if h.shouldDirectProxy(r) {
		h.directProxy(w, r)

		return
	}

	h.enqueueRequest(w, r)
}

func isChunkedTransfer(r *http.Request) bool {
	for _, te := range r.TransferEncoding {
		if strings.EqualFold(te, "chunked") {
			return true
		}
	}

	return false
}

func (h *Handler) shouldDirectProxy(
	r *http.Request,
) bool {
	if isChunkedTransfer(r) {
		return true
	}

	if h.directProxyThreshold > 0 &&
		r.ContentLength > h.directProxyThreshold {
		return true
	}

	return h.matchesDirectProxyPath(r.URL.Path)
}

func (h *Handler) matchesDirectProxyPath(
	path string,
) bool {
	for _, re := range h.directProxyPaths {
		if re.MatchString(path) {
			return true
		}
	}

	return false
}

func (h *Handler) directProxy(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := slogging.GetLogger(r.Context())

	if h.directProxyMode == DirectProxyModeRedirect {
		target := h.upstreamURL + r.RequestURI

		logger.Debug("redirecting to upstream",
			"target", target,
		)

		http.Redirect(
			w, r, target,
			http.StatusTemporaryRedirect,
		)

		return
	}

	if h.reverseProxy == nil {
		logger.Error("reverse proxy not configured")
		proxylib.WriteError(
			w, http.StatusBadGateway,
		)

		return
	}

	logger.Debug("direct proxying request",
		"content_length", r.ContentLength,
		"chunked", isChunkedTransfer(r),
	)

	h.reverseProxy.ServeHTTP(w, r)
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
) {
	if h.reverseProxy == nil {
		logger := slogging.GetLogger(r.Context())
		logger.Error(
			"websocket proxy not configured",
		)
		proxylib.WriteError(
			w, http.StatusBadGateway,
		)

		return
	}

	logger := slogging.GetLogger(r.Context())
	logger.Debug("proxying websocket connection")

	h.reverseProxy.ServeHTTP(w, r)
}

func (h *Handler) enqueueRequest(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := slogging.GetLogger(r.Context())

	body, err := io.ReadAll(
		io.LimitReader(r.Body, h.maxRequestBodySize),
	)
	if err != nil {
		logger.Error(
			"failed to read request body",
			"error", err,
		)
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return
	}

	payload := proxylib.RequestPayload{
		Method:   r.Method,
		URL:      h.upstreamURL + r.RequestURI,
		Headers:  r.Header,
		Body:     body,
		ClientIP: aichteeteapee.GetClientIP(r),
		Proto:    proxylib.RequestScheme(r),
	}

	taskID, err := h.enqueue(r, payload)
	if err != nil {
		logger.Error(
			"failed to enqueue task",
			"error", err,
		)
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return
	}

	aichteeteapee.WriteJSON(
		w,
		http.StatusAccepted,
		acceptedResponse{JobID: taskID},
	)
}

func (h *Handler) enqueue(
	r *http.Request,
	payload proxylib.RequestPayload,
) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", ctxerrors.Wrap(
			err, "marshal payload",
		)
	}

	taskID := uuid.New().String()
	task := asynq.NewTask(TaskTypeName, data)

	_, err = h.client.EnqueueContext(
		r.Context(),
		task,
		asynq.TaskID(taskID),
		asynq.Queue(h.queue),
		asynq.MaxRetry(0),
		asynq.Retention(h.taskRetention),
	)
	if err != nil {
		return "", ctxerrors.Wrap(
			err, "enqueue task",
		)
	}

	return taskID, nil
}
