package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/sse"
	"github.com/Thiht/pici/internal/stores"
)

type ExecutionsHandler struct {
	store           stores.Store
	runner          *ci.Runner
	maxSnapshotSize int64
	projects        *projects.Module
	executions      *executions.Module
}

type ExecutionsDeps struct {
	Store           stores.Store
	Runner          *ci.Runner
	MaxSnapshotSize int64
	Projects        *projects.Module
	Executions      *executions.Module
}

func NewExecutionsHandler(d ExecutionsDeps) *ExecutionsHandler {
	return &ExecutionsHandler{
		store:           d.Store,
		runner:          d.Runner,
		maxSnapshotSize: d.MaxSnapshotSize,
		projects:        d.Projects,
		executions:      d.Executions,
	}
}

type executionRequest struct {
	Workflow string `json:"workflow"`
	Ref      string `json:"ref"`
}

func (h *ExecutionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}

	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
		h.createSnapshot(w, r, project)
		return
	}

	var req executionRequest
	if err := bind.JSON(r.Body, &req); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	execution, err := h.executions.Create(r.Context(), project, req.Workflow, req.Ref, auth.UserID(r.Context()))
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}

func (h *ExecutionsHandler) createSnapshot(w http.ResponseWriter, r *http.Request, project stores.Project) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxSnapshotSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			render.Error(w, http.StatusRequestEntityTooLarge, errors.New("snapshot exceeds maximum size"))
			return
		}
		render.Error(w, http.StatusBadRequest, err)
		return
	}

	file, _, err := r.FormFile("snapshot")
	if err != nil {
		render.Error(w, http.StatusBadRequest, errors.New("snapshot file is required"))
		return
	}
	defer file.Close()

	execution, err := h.executions.CreateSnapshot(r.Context(), project, r.FormValue("workflow"), r.FormValue("ref"), r.FormValue("commit_sha"), file, auth.UserID(r.Context()))
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}

func (h *ExecutionsHandler) Rebuild(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	execution, err := h.executions.Rebuild(r.Context(), project, id, auth.UserID(r.Context()))
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}

func (h *ExecutionsHandler) Retry(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	execution, err := h.executions.Retry(r.Context(), project, id, attempt, auth.UserID(r.Context()))
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}

func (h *ExecutionsHandler) List(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	executions, err := h.store.ListExecutions(r.Context(), project.ID, limit)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusOK, executions)
}

func (h *ExecutionsHandler) ListAttempts(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	attempts, err := h.store.ListAttempts(r.Context(), project.ID, id)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if len(attempts) == 0 {
		render.Error(w, http.StatusNotFound, stores.ErrNotFound)
		return
	}
	out := make([]stores.Attempt, 0, len(attempts))
	for _, e := range attempts {
		out = append(out, e.AttemptSummary())
	}
	render.JSON(w, http.StatusOK, out)
}

func (h *ExecutionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	view, err := h.executions.Get(r.Context(), project.ID, id, attempt)
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, view.Execution)
}

func (h *ExecutionsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	view, err := h.executions.Get(r.Context(), project.ID, id, attempt)
	if err != nil {
		storeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	logs := h.executions.Logs(view.Execution)
	defer logs.Close()
	_, _ = io.Copy(w, logs)
}

func (h *ExecutionsHandler) StepLogs(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	step := r.PathValue("step")

	view, err := h.executions.Get(r.Context(), project.ID, id, attempt)
	if err != nil {
		storeError(w, err)
		return
	}
	logs, err := h.executions.StepLog(view.Execution, step)
	if err != nil {
		storeError(w, err)
		return
	}
	defer logs.Close()
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.Copy(w, logs)
}

func (h *ExecutionsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	if err := h.executions.Cancel(r.Context(), project.ID, id); err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, map[string]string{"status": "canceling"})
}

func (h *ExecutionsHandler) LogStream(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	view, err := h.executions.Get(r.Context(), project.ID, id, attempt)
	if err != nil {
		storeError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		render.Error(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	stream := sse.New(w, flusher.Flush)
	if err := h.runner.StreamLogs(r.Context(), project.ID, view.Execution.ID, stream.Send); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("stream execution logs", "error", err)
	}
}
