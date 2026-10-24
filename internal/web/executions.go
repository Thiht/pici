package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/stores"
)

type stepView struct {
	Index     int
	LogSource string
	stores.StepResult
	Logs string
}

// TimeoutExceeded reports whether a failed step ran longer than its timeout.
func (s stepView) TimeoutExceeded() bool {
	return s.Status == stores.StepStatusFailed && s.Timeout > 0 && s.StartedAt != nil && s.FinishedAt != nil &&
		s.FinishedAt.Sub(*s.StartedAt) > s.Timeout
}

type envVar struct {
	Key   string
	Value string
}

func splitEnv(kv string) envVar {
	key, value, _ := strings.Cut(kv, "=")
	return envVar{Key: key, Value: value}
}

type executionPage struct {
	base
	Execution stores.Execution
	Project   stores.Project
	Artifacts []ci.Artifact
	Steps     []stepView
	SetupLog  string
	Running   bool
	Attempts  []stores.Execution
	IsLatest  bool
	Error     string
}

func (h *Handler) ExecutionCreate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	workflow := strings.TrimSpace(r.FormValue("workflow"))
	if workflow == "" {
		setFlash(w, "error", "Workflow is required.")
		redirect(w, r, "/projects/"+project.ID.String())
		return
	}
	ref := strings.TrimSpace(r.FormValue("ref"))
	if ref == "" {
		ref = project.DefaultBranch
	}
	execution, err := h.runner.Enqueue(r.Context(), project, workflow, ref, "", stores.TriggerManual, auth.UserID(r.Context()))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Execution queued.")
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(execution.ExecutionID, 10))
}

func (h *Handler) ExecutionShow(w http.ResponseWriter, r *http.Request) {
	data, ok := h.executionData(w, r)
	if !ok {
		return
	}
	h.render(w, r, http.StatusOK, "execution_show", data)
}

func (h *Handler) executionData(w http.ResponseWriter, r *http.Request) (executionPage, bool) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return executionPage{}, false
	}
	execution, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return executionPage{}, false
	}
	attempts, err := h.store.ListAttempts(r.Context(), project.ID, execution.ExecutionID)
	if err != nil {
		h.storeError(w, r, err)
		return executionPage{}, false
	}
	running := execution.Status == stores.StatusRunning || execution.Status == stores.StatusPending
	steps := make([]stepView, len(execution.Steps))
	for i, sr := range execution.Steps {
		path := h.runner.StepLogPath(project.ID, execution.ID, i, sr.Name)
		sv := stepView{Index: i, LogSource: ci.LogSource(path), StepResult: sr}
		if !running {
			sv.Logs = readFile(path)
		}
		steps[i] = sv
	}
	var setupLog string
	if !running {
		setupLog = readFile(h.runner.SetupLogPath(project.ID, execution.ID))
	}
	return executionPage{
		base:      h.base(w, r, "Execution #"+strconv.FormatInt(execution.ExecutionID, 10), "projects"),
		Execution: execution,
		Project:   project,
		Artifacts: h.runner.ListArtifacts(execution),
		Steps:     steps,
		SetupLog:  setupLog,
		Running:   running,
		Attempts:  attempts,
		IsLatest:  execution.Attempt == execution.AttemptsCount,
	}, true
}

// resolveAttempt resolves the execution id from the path and the optional
// ?attempt= selector to the concrete attempt row.
func (h *Handler) resolveAttempt(w http.ResponseWriter, r *http.Request, project stores.Project) (stores.Execution, bool) {
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return stores.Execution{}, false
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		h.notFound(w, r)
		return stores.Execution{}, false
	}
	execution, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, attempt)
	if err != nil {
		h.storeError(w, r, err)
		return stores.Execution{}, false
	}
	return execution, true
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func (h *Handler) ExecutionLogs(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	execution, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, path := range h.runner.LogPaths(project.ID, execution.ID) {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		_, _ = io.Copy(w, f)
		_ = f.Close()
	}
}

func (h *Handler) ExecutionStream(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	execution, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.serverError(w, r, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	h.runner.StreamLogs(r.Context(), project.ID, execution.ID, w, flusher.Flush)
}

func (h *Handler) StepLogs(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	execution, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return
	}
	step := r.PathValue("step")
	index := stepIndex(execution, step)
	if index < 0 {
		h.notFound(w, r)
		return
	}
	f, err := os.Open(h.runner.StepLogPath(project.ID, execution.ID, index, step))
	if err != nil {
		h.notFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.Copy(w, f)
}

func (h *Handler) ExecutionCancel(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return
	}
	execution, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, 0)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	if err := h.store.SetCancelRequested(r.Context(), project.ID, execution.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.runner.Cancel(project.ID, execution.ID)
	setFlash(w, "success", "Cancellation requested.")
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(execution.ExecutionID, 10))
}

func (h *Handler) ExecutionRebuild(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return
	}
	previous, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, 0)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	if previous.Source == stores.SourceSnapshot {
		setFlash(w, "error", "Snapshot executions cannot be rebuilt.")
		redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(previous.ExecutionID, 10))
		return
	}
	execution, err := h.runner.EnqueueRebuild(r.Context(), project, previous, auth.UserID(r.Context()))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Rebuild queued.")
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(execution.ExecutionID, 10))
}

func (h *Handler) ExecutionRetry(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		h.notFound(w, r)
		return
	}
	previous, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, attempt)
	if err != nil {
		h.storeError(w, r, err)
		return
	}

	_, err = h.runner.EnqueueRetry(r.Context(), project, previous, auth.UserID(r.Context()))
	switch {
	case errors.Is(err, ci.ErrWorkspaceGone):
		setFlash(w, "error", "The workspace is no longer available.")
	case errors.Is(err, ci.ErrNotRetryable):
		setFlash(w, "error", "This execution cannot be retried.")
	case err != nil:
		h.serverError(w, r, err)
		return
	default:
		setFlash(w, "success", "Retry queued.")
	}
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(previous.ExecutionID, 10))
}

func (h *Handler) ArtifactDownload(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	execution, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return
	}
	index := stepIndex(execution, r.PathValue("step"))
	if index < 0 {
		h.notFound(w, r)
		return
	}
	dir := filepath.Join(h.runner.ArtifactDir(project.ID, execution.ID), fmt.Sprintf("%03d", index))
	full := filepath.Clean(filepath.Join(dir, filepath.FromSlash(r.PathValue("path"))))
	if full != dir && !strings.HasPrefix(full, dir+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		h.notFound(w, r)
		return
	}
	relPath, err := filepath.Rel(dir, full)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if etag := h.runner.ArtifactETag(execution, index, filepath.ToSlash(relPath)); etag != "" {
		w.Header().Set("ETag", etag)
	}
	http.ServeFile(w, r, full)
}

func stepIndex(execution stores.Execution, name string) int {
	for i, sr := range execution.Steps {
		if sr.Name == name {
			return i
		}
	}
	return -1
}
