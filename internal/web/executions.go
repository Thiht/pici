package web

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

type artifact struct {
	Step string
	Path string
	Size int64
}

type stepView struct {
	Index     int
	LogSource string
	stores.StepResult
	Logs string
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
	Artifacts []artifact
	Steps     []stepView
	SetupLog  string
	Running   bool
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
	execution, err := h.runner.Enqueue(r.Context(), project, workflow, ref, "", stores.TriggerManual)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Execution queued.")
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(execution.ID, 10))
}

func (h *Handler) ExecutionShow(w http.ResponseWriter, r *http.Request) {
	data, err := h.executionData(w, r)
	if err != nil {
		return
	}
	h.render(w, r, http.StatusOK, "execution_show", data)
}

func (h *Handler) executionData(w http.ResponseWriter, r *http.Request) (executionPage, error) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return executionPage{}, err
	}
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return executionPage{}, err
	}
	execution, err := h.store.GetExecution(r.Context(), project.ID, id)
	if err != nil {
		h.storeError(w, r, err)
		return executionPage{}, err
	}
	running := execution.Status == stores.StatusRunning || execution.Status == stores.StatusPending
	steps := make([]stepView, len(execution.Steps))
	for i, sr := range execution.Steps {
		path := h.runner.StepLogPath(project.ID, id, i, sr.Name)
		sv := stepView{Index: i, LogSource: ci.LogSource(path), StepResult: sr}
		if !running {
			sv.Logs = readFile(path)
		}
		steps[i] = sv
	}
	var setupLog string
	if !running {
		setupLog = readFile(h.runner.SetupLogPath(project.ID, id))
	}
	return executionPage{
		base:      h.base(w, r, "Execution #"+strconv.FormatInt(id, 10), "projects"),
		Execution: execution,
		Project:   project,
		Artifacts: h.listArtifacts(execution),
		Steps:     steps,
		SetupLog:  setupLog,
		Running:   running,
	}, nil
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
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return
	}
	if _, err := h.store.GetExecution(r.Context(), project.ID, id); err != nil {
		h.storeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, path := range h.runner.LogPaths(project.ID, id) {
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
	id, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return
	}
	if _, err := h.store.GetExecution(r.Context(), project.ID, id); err != nil {
		h.storeError(w, r, err)
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
	h.runner.StreamLogs(r.Context(), project.ID, id, w, flusher.Flush)
}

func (h *Handler) StepLogs(w http.ResponseWriter, r *http.Request) {
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
	step := r.PathValue("step")
	execution, err := h.store.GetExecution(r.Context(), project.ID, id)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	index := stepIndex(execution, step)
	if index < 0 {
		h.notFound(w, r)
		return
	}
	f, err := os.Open(h.runner.StepLogPath(project.ID, id, index, step))
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
	if _, err := h.store.GetExecution(r.Context(), project.ID, id); err != nil {
		h.storeError(w, r, err)
		return
	}
	if err := h.store.SetCancelRequested(r.Context(), project.ID, id); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.runner.Cancel(project.ID, id)
	setFlash(w, "success", "Cancellation requested.")
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(id, 10))
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
	previous, err := h.store.GetExecution(r.Context(), project.ID, id)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	if previous.Source == stores.SourceSnapshot {
		setFlash(w, "error", "Snapshot executions cannot be rebuilt.")
		redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(id, 10))
		return
	}
	execution, err := h.runner.EnqueueRebuild(r.Context(), project, previous)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Rebuild queued.")
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(execution.ID, 10))
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
	previous, err := h.store.GetExecution(r.Context(), project.ID, id)
	if err != nil {
		h.storeError(w, r, err)
		return
	}

	_, err = h.runner.EnqueueRetry(r.Context(), project, previous)
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
	redirect(w, r, "/projects/"+project.ID.String()+"/executions/"+strconv.FormatInt(id, 10))
}

func (h *Handler) ArtifactDownload(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	execID, err := strconv.ParseInt(r.PathValue("executionID"), 10, 64)
	if err != nil {
		h.notFound(w, r)
		return
	}
	execution, err := h.store.GetExecution(r.Context(), project.ID, execID)
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	index := stepIndex(execution, r.PathValue("step"))
	if index < 0 {
		h.notFound(w, r)
		return
	}
	dir := filepath.Join(h.runner.ArtifactDir(project.ID, execID), fmt.Sprintf("%03d", index))
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
	http.ServeFile(w, r, full)
}

func (h *Handler) listArtifacts(execution stores.Execution) []artifact {
	dir := h.runner.ArtifactDir(execution.ProjectID, execution.ID)
	out := []artifact{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) != 2 {
			return nil
		}
		var size int64
		if info, err := d.Info(); err == nil {
			size = info.Size()
		}
		out = append(out, artifact{Step: stepNameForIndex(execution, parts[0]), Path: filepath.ToSlash(parts[1]), Size: size})
		return nil
	})
	return out
}

func stepIndex(execution stores.Execution, name string) int {
	for i, sr := range execution.Steps {
		if sr.Name == name {
			return i
		}
	}
	return -1
}

func stepNameForIndex(execution stores.Execution, indexDir string) string {
	if idx, err := strconv.Atoi(indexDir); err == nil && idx >= 0 && idx < len(execution.Steps) {
		return execution.Steps[idx].Name
	}
	return indexDir
}
