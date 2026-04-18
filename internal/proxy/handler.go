package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee"
	proxylib "github.com/psyb0t/aichteeteapee/server/proxy"
	"github.com/psyb0t/ctxerrors"
)

const defaultMaxRequestBodySize = 10 << 20 // 10MB
const defaultTaskRetention = 1 * time.Hour

type HandlerConfig struct {
	UpstreamURL        string
	MaxRequestBodySize int64
	Queue              string
	TaskRetention      time.Duration
	Logger             *slog.Logger
}

type Handler struct {
	client             *asynq.Client
	upstreamURL        string
	queue              string
	taskRetention      time.Duration
	logger             *slog.Logger
	maxRequestBodySize int64
}

func NewHandler(
	client *asynq.Client,
	cfg HandlerConfig,
) *Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

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

	return &Handler{
		client:             client,
		upstreamURL:        cfg.UpstreamURL,
		queue:              queue,
		taskRetention:      retention,
		logger:             logger,
		maxRequestBodySize: maxBody,
	}
}

type acceptedResponse struct {
	JobID string `json:"jobId"`
}

func (h *Handler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	body, err := io.ReadAll(
		io.LimitReader(r.Body, h.maxRequestBodySize),
	)
	if err != nil {
		h.logger.Error(
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
		h.logger.Error(
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
