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

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type ExecutionsHandler struct {
	store           stores.Store
	runner          *ci.Runner
	workspaceDir    string
	maxSnapshotSize int64
}

func NewExecutionsHandler(store stores.Store, runner *ci.Runner, workspaceDir string, maxSnapshotSize int64) *ExecutionsHandler {
	return &ExecutionsHandler{store: store, runner: runner, workspaceDir: workspaceDir, maxSnapshotSize: maxSnapshotSize}
}

type executionRequest struct {
	Workflow string `json:"workflow"`
	Ref      string `json:"ref"`
}

func (h *ExecutionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
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

	execution, err := h.runner.Enqueue(r.Context(), project, req.Workflow, req.Ref, "", stores.TriggerManual)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}

func (h *ExecutionsHandler) createSnapshot(w http.ResponseWriter, r *http.Request, project stores.Project) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxSnapshotSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
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
	dir := filepath.Join(h.workspaceDir, "uploads", project.ID.String())
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

	execution, err := h.runner.EnqueueSnapshot(r.Context(), project, workflow, ref, r.FormValue("commit_sha"), snapshotID)
	if err != nil {
		_ = os.Remove(dst)
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}

func (h *ExecutionsHandler) Rebuild(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	previous, err := h.store.GetExecution(r.Context(), project.ID, id)
	if err != nil {
		storeError(w, err)
		return
	}

	if previous.Source == stores.SourceSnapshot {
		render.Error(w, http.StatusConflict, errors.New("snapshot executions cannot be rebuilt"))
		return
	}

	execution, err := h.runner.Enqueue(r.Context(), project, previous.Workflow, previous.Ref, previous.CommitSHA, stores.TriggerRebuild)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusAccepted, execution)
}

func (h *ExecutionsHandler) List(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
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

func (h *ExecutionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	execution, err := h.store.GetExecution(r.Context(), project.ID, id)
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, execution)
}

func (h *ExecutionsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	if _, err := h.store.GetExecution(r.Context(), project.ID, id); err != nil {
		storeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	for _, path := range h.runner.LogPaths(project.ID, id) {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		_, _ = io.Copy(w, f)
		_ = f.Close()
	}
}

func (h *ExecutionsHandler) StepLogs(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	step := r.PathValue("step")

	execution, err := h.store.GetExecution(r.Context(), project.ID, id)
	if err != nil {
		storeError(w, err)
		return
	}
	index := -1
	for i, sr := range execution.Steps {
		if sr.Name == step {
			index = i
			break
		}
	}
	if index < 0 {
		render.Error(w, http.StatusNotFound, errors.New("step not found"))
		return
	}

	f, err := os.Open(h.runner.StepLogPath(project.ID, id, index, step))
	if err != nil {
		render.Error(w, http.StatusNotFound, errors.New("step logs not found"))
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.Copy(w, f)
}

func (h *ExecutionsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	if _, err := h.store.GetExecution(r.Context(), project.ID, id); err != nil {
		storeError(w, err)
		return
	}
	if err := h.store.SetCancelRequested(r.Context(), project.ID, id); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	h.runner.Cancel(project.ID, id)
	render.JSON(w, http.StatusOK, map[string]string{"status": "canceling"})
}

func (h *ExecutionsHandler) LogStream(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	execID, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	if _, err := h.store.GetExecution(r.Context(), project.ID, execID); err != nil {
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

	h.runner.StreamLogs(r.Context(), project.ID, execID, w, flusher.Flush)
}
