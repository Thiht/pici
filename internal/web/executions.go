package web

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/app/artifacts"
	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
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
	Artifacts []artifacts.Artifact
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
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
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
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return executionPage{}, false
	}
	view, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return executionPage{}, false
	}
	execution := view.Execution
	running := execution.Status == stores.StatusRunning || execution.Status == stores.StatusPending
	steps := make([]stepView, len(execution.Steps))
	for i, sr := range execution.Steps {
		sv := stepView{Index: i, LogSource: workspace.Source(sr.Name), StepResult: sr}
		if !running {
			if logs, err := h.executions.StepLog(execution, sr.Name); err == nil {
				sv.Logs = readAll(logs)
			}
		}
		steps[i] = sv
	}
	var setupLog string
	if !running {
		if logs, err := h.executions.SetupLog(execution); err == nil {
			setupLog = readAll(logs)
		}
	}
	return executionPage{
		base:      h.base(w, r, "Execution #"+strconv.FormatInt(execution.ExecutionID, 10), "projects"),
		Execution: execution,
		Project:   project,
		Artifacts: h.artifacts.List(execution),
		Steps:     steps,
		SetupLog:  setupLog,
		Running:   running,
		Attempts:  view.Attempts,
		IsLatest:  view.Latest,
	}, true
}

// resolveAttempt resolves the Execution id from the path and the optional
// ?attempt= selector to the Attempt the request is about.
func (h *Handler) resolveAttempt(w http.ResponseWriter, r *http.Request, project stores.Project) (executions.View, bool) {
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return executions.View{}, false
	}
	attempt, err := bind.Attempt(r)
	if err != nil {
		h.notFound(w, r)
		return executions.View{}, false
	}
	view, err := h.executions.Get(r.Context(), project.ID, id, attempt)
	if err != nil {
		h.storeError(w, r, err)
		return executions.View{}, false
	}
	return view, true
}

func readAll(logs io.ReadCloser) string {
	defer logs.Close()
	b, err := io.ReadAll(logs)
	if err != nil {
		return ""
	}
	return string(b)
}

func (h *Handler) ExecutionLogs(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	view, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	logs := h.executions.Logs(view.Execution)
	defer logs.Close()
	_, _ = io.Copy(w, logs)
}

func (h *Handler) ExecutionStream(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	view, ok := h.resolveAttempt(w, r, project)
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
	h.runner.StreamLogs(r.Context(), project.ID, view.Execution.ID, w, flusher.Flush)
}

func (h *Handler) StepLogs(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	view, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return
	}
	step := r.PathValue("step")
	logs, err := h.executions.StepLog(view.Execution, step)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	defer logs.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.Copy(w, logs)
}

func (h *Handler) ExecutionCancel(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
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
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
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
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
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
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	view, ok := h.resolveAttempt(w, r, project)
	if !ok {
		return
	}
	file, err := h.artifacts.Open(view.Execution, r.PathValue("step"), r.PathValue("path"))
	if err != nil {
		if errors.Is(err, app.ErrInvalid) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		h.notFound(w, r)
		return
	}
	defer file.Close()
	if file.ETag != "" {
		w.Header().Set("ETag", file.ETag)
	}
	http.ServeContent(w, r, filepath.Base(r.PathValue("path")), file.Info.ModTime(), file)
}
