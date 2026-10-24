package handlers

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/app/artifacts"
	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
)

type ArtifactsHandler struct {
	projects   *projects.Module
	executions *executions.Module
	artifacts  *artifacts.Module
}

func NewArtifactsHandler(projects *projects.Module, executions *executions.Module, artifacts *artifacts.Module) *ArtifactsHandler {
	return &ArtifactsHandler{projects: projects, executions: executions, artifacts: artifacts}
}

func (h *ArtifactsHandler) List(w http.ResponseWriter, r *http.Request) {
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
	render.JSON(w, http.StatusOK, h.artifacts.List(view.Execution))
}

func (h *ArtifactsHandler) Download(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
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
	view, err := h.executions.Get(r.Context(), project.ID, execID, attempt)
	if err != nil {
		storeError(w, err)
		return
	}

	file, err := h.artifacts.Open(view.Execution, r.PathValue("step"), r.PathValue("path"))
	if err != nil {
		if errors.Is(err, app.ErrInvalid) {
			render.Error(w, http.StatusBadRequest, errors.New("invalid path"))
			return
		}
		render.Error(w, http.StatusNotFound, errors.New("artifact not found"))
		return
	}
	defer file.Close()
	if file.ETag != "" {
		w.Header().Set("ETag", file.ETag)
	}
	http.ServeContent(w, r, filepath.Base(r.PathValue("path")), file.Info.ModTime(), file)
}
