package handlers

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"uuid"

	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

type ExecutionsHandler struct {
	store           stores.Store
	runner          *ci.Runner
	workspace       *workspace.Workspace
	maxSnapshotSize int64
	projects        *projects.Module
	executions      *executions.Module
}

type ExecutionsDeps struct {
	Store           stores.Store
	Runner          *ci.Runner
	Workspace       *workspace.Workspace
	MaxSnapshotSize int64
	Projects        *projects.Module
	Executions      *executions.Module
}

func NewExecutionsHandler(d ExecutionsDeps) *ExecutionsHandler {
	return &ExecutionsHandler{
		store:           d.Store,
		runner:          d.Runner,
		workspace:       d.Workspace,
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
	if req.Workflow == "" {
		render.Error(w, http.StatusBadRequest, errors.New("workflow is required"))
		return
	}
	if req.Ref == "" {
		req.Ref = project.DefaultBranch
	}

	execution, err := h.runner.Enqueue(r.Context(), project, req.Workflow, req.Ref, "", stores.TriggerManual, auth.UserID(r.Context()))
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
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

	workflow := strings.TrimSpace(r.FormValue("workflow"))
	if workflow == "" {
		render.Error(w, http.StatusBadRequest, errors.New("workflow is required"))
		return
	}
	ref := r.FormValue("ref")
	if ref == "" {
		ref = project.DefaultBranch
	}

	file, _, err := r.FormFile("snapshot")
	if err != nil {
		render.Error(w, http.StatusBadRequest, errors.New("snapshot file is required"))
		return
	}
	defer file.Close()

	snapshotID := uuid.New()
	dir := h.workspace.UploadsDir(project.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	tmp := filepath.Join(dir, snapshotID.String()+".tar.gz.tmp")
	dst := filepath.Join(dir, snapshotID.String()+".tar.gz")
	out, err := os.Create(tmp)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}

	execution, err := h.runner.EnqueueSnapshot(r.Context(), project, workflow, ref, r.FormValue("commit_sha"), snapshotID, auth.UserID(r.Context()))
	if err != nil {
		_ = os.Remove(dst)
		render.Error(w, http.StatusInternalServerError, err)
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
	previous, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, 0)
	if err != nil {
		storeError(w, err)
		return
	}

	if previous.Source == stores.SourceSnapshot {
		render.Error(w, http.StatusConflict, errors.New("snapshot executions cannot be rebuilt"))
		return
	}

	execution, err := h.runner.EnqueueRebuild(r.Context(), project, previous, auth.UserID(r.Context()))
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
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
	previous, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, attempt)
	if err != nil {
		storeError(w, err)
		return
	}

	_, err = h.runner.EnqueueRetry(r.Context(), project, previous, auth.UserID(r.Context()))
	switch {
	case errors.Is(err, ci.ErrWorkspaceGone):
		render.Error(w, http.StatusGone, err)
		return
	case errors.Is(err, ci.ErrNotRetryable):
		render.Error(w, http.StatusConflict, err)
		return
	case err != nil:
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	// Return the Execution as seen by the API: origin metadata plus the new
	// latest attempt, not the raw attempt row that was just inserted.
	execution, err := h.store.GetExecutionAttempt(r.Context(), project.ID, previous.ExecutionID, 0)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
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
	execution, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, 0)
	if err != nil {
		storeError(w, err)
		return
	}
	if err := h.store.SetCancelRequested(r.Context(), project.ID, execution.ID); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	h.runner.Cancel(project.ID, execution.ID)
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

	h.runner.StreamLogs(r.Context(), project.ID, view.Execution.ID, w, flusher.Flush)
}
