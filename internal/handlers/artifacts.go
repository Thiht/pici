package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type ArtifactsHandler struct {
	store  stores.Store
	runner *ci.Runner
}

func NewArtifactsHandler(store stores.Store, runner *ci.Runner) *ArtifactsHandler {
	return &ArtifactsHandler{store: store, runner: runner}
}

func (h *ArtifactsHandler) List(w http.ResponseWriter, r *http.Request) {
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
	attempt, err := bind.Attempt(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	execution, err := h.store.GetExecutionAttempt(r.Context(), project.ID, id, attempt)
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, h.runner.ListArtifacts(execution))
}

func (h *ArtifactsHandler) Download(w http.ResponseWriter, r *http.Request) {
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
	attempt, err := bind.Attempt(r)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	step := r.PathValue("step")
	rel := r.PathValue("path")

	execution, err := h.store.GetExecutionAttempt(r.Context(), project.ID, execID, attempt)
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

	dir := filepath.Join(h.runner.ArtifactDir(project.ID, execution.ID), fmt.Sprintf("%03d", index))
	full := filepath.Clean(filepath.Join(dir, filepath.FromSlash(rel)))
	if full != dir && !strings.HasPrefix(full, dir+string(filepath.Separator)) {
		render.Error(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}

	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		render.Error(w, http.StatusNotFound, errors.New("artifact not found"))
		return
	}
	relPath, err := filepath.Rel(dir, full)
	if err != nil {
		render.Error(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	if etag := h.runner.ArtifactETag(execution, index, filepath.ToSlash(relPath)); etag != "" {
		w.Header().Set("ETag", etag)
	}
	http.ServeFile(w, r, full)
}
