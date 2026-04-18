package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/hibiken/asynq"
	"github.com/psyb0t/aichteeteapee"
	proxylib "github.com/psyb0t/aichteeteapee/serbewr/prawxxey"
	"github.com/psyb0t/common-go/slogging"
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

	info, err := h.inspector.GetTaskInfo(
		h.queue, id,
	)
	if errors.Is(err, asynq.ErrTaskNotFound) {
		aichteeteapee.WriteJSON(
			w,
			http.StatusNotFound,
			aichteeteapee.ErrorResponseNotFound,
		)

		return
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

		return
	}

	job := JobInfo{
		ID:          info.ID,
		Status:      StatusFromTaskState(info.State),
		Error:       info.LastErr,
		CompletedAt: info.CompletedAt,
	}

	if len(info.Result) > 0 {
		var result proxylib.ResponseResult
		if err := json.Unmarshal(
			info.Result, &result,
		); err == nil {
			job.Result = &result
		}
	}

	aichteeteapee.WriteJSON(w, http.StatusOK, job)
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
		map[string]string{"status": "cancelled"},
	)
}

func extractJobID(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}

	path := strings.TrimPrefix(
		r.URL.Path, "/__jobs/",
	)

	return strings.TrimSuffix(path, "/")
}
