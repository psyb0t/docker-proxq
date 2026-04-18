package proxy

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee"
	proxylib "github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	"github.com/psyb0t/common-go/slogging"
	"github.com/psyb0t/ctxerrors"
)

type JobsHandler struct {
	inspector *asynq.Inspector
	queue     string
}

func NewJobsHandler(
	inspector *asynq.Inspector,
	queue string,
) *JobsHandler {
	if queue == "" {
		queue = DefaultQueue
	}

	return &JobsHandler{
		inspector: inspector,
		queue:     queue,
	}
}

func (h *JobsHandler) Get(
	w http.ResponseWriter,
	r *http.Request,
) {
	id := extractJobID(r)
	if id == "" {
		aichteeteapee.WriteJSON(
			w,
			http.StatusBadRequest,
			aichteeteapee.ErrorResponseBadRequest,
		)

		return
	}

	info, err := h.getTaskInfo(w, r, id)
	if err != nil {
		return
	}

	job := jobInfo{
		ID:          info.ID,
		Status:      StatusFromTaskState(info.State),
		Error:       info.LastErr,
		CompletedAt: info.CompletedAt,
	}

	aichteeteapee.WriteJSON(w, http.StatusOK, job)
}

func (h *JobsHandler) Content(
	w http.ResponseWriter,
	r *http.Request,
) {
	id := extractJobID(r)
	if id == "" {
		aichteeteapee.WriteJSON(
			w,
			http.StatusBadRequest,
			aichteeteapee.ErrorResponseBadRequest,
		)

		return
	}

	info, err := h.getTaskInfo(w, r, id)
	if err != nil {
		return
	}

	status := StatusFromTaskState(info.State)
	if status != StatusCompleted {
		aichteeteapee.WriteJSON(
			w,
			http.StatusNotFound,
			aichteeteapee.ErrorResponseNotFound,
		)

		return
	}

	if len(info.Result) == 0 {
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return
	}

	var result proxylib.ResponseResult
	if err := json.Unmarshal(
		info.Result, &result,
	); err != nil {
		logger := slogging.GetLogger(r.Context())
		logger.Error(
			"failed to unmarshal result",
			"job_id", id,
			"error", err,
		)
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return
	}

	writeUpstreamResponse(w, &result)
}

func writeUpstreamResponse(
	w http.ResponseWriter,
	result *proxylib.ResponseResult,
) {
	for key, vals := range result.Headers {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}

	w.WriteHeader(result.StatusCode)

	if len(result.Body) > 0 {
		_, _ = w.Write(result.Body)
	}
}

func (h *JobsHandler) getTaskInfo(
	w http.ResponseWriter,
	r *http.Request,
	id string,
) (*asynq.TaskInfo, error) {
	info, err := h.inspector.GetTaskInfo(
		h.queue, id,
	)
	if errors.Is(err, asynq.ErrTaskNotFound) {
		aichteeteapee.WriteJSON(
			w,
			http.StatusNotFound,
			aichteeteapee.ErrorResponseNotFound,
		)

		return nil, ctxerrors.Wrap(err, "task not found")
	}

	if err != nil {
		logger := slogging.GetLogger(r.Context())
		logger.Error(
			"failed to get task info",
			"job_id", id,
			"error", err,
		)
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return nil, ctxerrors.Wrap(err, "get task info")
	}

	return info, nil
}

func (h *JobsHandler) Cancel(
	w http.ResponseWriter,
	r *http.Request,
) {
	id := extractJobID(r)
	if id == "" {
		aichteeteapee.WriteJSON(
			w,
			http.StatusBadRequest,
			aichteeteapee.ErrorResponseBadRequest,
		)

		return
	}

	logger := slogging.GetLogger(r.Context())

	if err := h.inspector.CancelProcessing(
		id,
	); err != nil {
		logger.Debug(
			"cancel processing failed",
			"job_id", id,
			"error", err,
		)
	}

	err := h.inspector.DeleteTask(h.queue, id)
	if errors.Is(err, asynq.ErrTaskNotFound) {
		aichteeteapee.WriteJSON(
			w,
			http.StatusNotFound,
			aichteeteapee.ErrorResponseNotFound,
		)

		return
	}

	if err != nil {
		logger.Error(
			"failed to delete task",
			"job_id", id,
			"error", err,
		)
		proxylib.WriteError(
			w, http.StatusInternalServerError,
		)

		return
	}

	aichteeteapee.WriteJSON(
		w,
		http.StatusOK,
		cancelledResponse,
	)
}

func extractJobID(r *http.Request) string {
	return r.PathValue("id")
}
