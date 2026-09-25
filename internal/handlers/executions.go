package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type ExecutionsHandler struct {
	store  stores.Store
	runner *ci.Runner
}

func NewExecutionsHandler(store stores.Store, runner *ci.Runner) *ExecutionsHandler {
	return &ExecutionsHandler{store: store, runner: runner}
}

type executionRequest struct {
	Workflow string `json:"workflow"`
	Ref      string `json:"ref"`
}

func (h *ExecutionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
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

func (h *ExecutionsHandler) Rebuild(w http.ResponseWriter, r *http.Request) {
	previous, err := h.store.GetExecution(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	project, err := h.store.GetProject(r.Context(), previous.ProjectID)
	if err != nil {
		writeStoreError(w, err)
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
		writeStoreError(w, err)
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
	execution, err := h.store.GetExecution(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, execution)
}

func (h *ExecutionsHandler) Logs(w http.ResponseWriter, r *http.Request) {
	if _, err := h.store.GetExecution(r.Context(), r.PathValue("id")); err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	for _, path := range h.runner.LogPaths(r.PathValue("id")) {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		_, _ = io.Copy(w, f)
		_ = f.Close()
	}
}

func (h *ExecutionsHandler) StepLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	step := r.PathValue("step")

	execution, err := h.store.GetExecution(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
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

	f, err := os.Open(h.runner.StepLogPath(id, index, step))
	if err != nil {
		render.Error(w, http.StatusNotFound, errors.New("step logs not found"))
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.Copy(w, f)
}

func (h *ExecutionsHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.store.GetExecution(r.Context(), id); err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	if err := h.store.SetCancelRequested(r.Context(), id); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	h.runner.Cancel(id)
	render.JSON(w, http.StatusOK, map[string]string{"status": "canceling"})
}

type streamState struct {
	offset  int64
	partial []byte
}

func (h *ExecutionsHandler) LogStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.store.GetExecution(r.Context(), id); err != nil {
		writeStoreError(w, err)
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

	states := map[string]*streamState{}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	emit := func(source string, line []byte) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", source, line)
	}

	for {
		sweep := func() {
			for _, path := range h.runner.LogPaths(id) {
				state := states[path]
				if state == nil {
					state = &streamState{}
					states[path] = state
				}
				fi, err := os.Stat(path)
				if err != nil || fi.Size() <= state.offset {
					continue
				}
				f, err := os.Open(path)
				if err != nil {
					continue
				}
				_, _ = f.Seek(state.offset, io.SeekStart)
				data, err := io.ReadAll(f)
				_ = f.Close()
				if err != nil || len(data) == 0 {
					continue
				}
				state.offset += int64(len(data))
				state.partial = append(state.partial, data...)
				for {
					idx := strings.IndexByte(string(state.partial), '\n')
					if idx < 0 {
						break
					}
					line := state.partial[:idx]
					state.partial = state.partial[idx+1:]
					emit(logSource(path), line)
				}
			}
		}

		sweep()

		execution, err := h.store.GetExecution(r.Context(), id)
		if err != nil {
			return
		}
		switch execution.Status {
		case stores.StatusSuccess, stores.StatusFailed, stores.StatusCanceled:
			sweep()
			for path, state := range states {
				if len(state.partial) > 0 {
					emit(logSource(path), state.partial)
				}
			}
			fmt.Fprintf(w, "event: done\ndata: %s\n\n", execution.Status)
			flusher.Flush()
			return
		}

		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func logSource(path string) string {
	base := filepath.Base(path)
	if base == "setup.log" {
		return "setup"
	}
	name := strings.TrimSuffix(base, ".log")
	if idx := strings.IndexByte(name, '_'); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}
