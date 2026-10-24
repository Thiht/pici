package handlers

import (
	"net/http"
	"uuid"

	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/app/variables"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type VariablesHandler struct {
	projects  *projects.Module
	variables *variables.Module
}

func NewVariablesHandler(projects *projects.Module, variables *variables.Module) *VariablesHandler {
	return &VariablesHandler{projects: projects, variables: variables}
}

type variableRequest struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

func (h *VariablesHandler) SetProject(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	h.set(w, r, &project.ID)
}

func (h *VariablesHandler) SetGlobal(w http.ResponseWriter, r *http.Request) {
	h.set(w, r, nil)
}

func (h *VariablesHandler) set(w http.ResponseWriter, r *http.Request, projectID *uuid.UUID) {
	var req variableRequest
	if err := bind.JSON(r.Body, &req); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	saved, err := h.variables.Set(r.Context(), stores.Variable{
		ProjectID: projectID,
		Key:       req.Key,
		Value:     req.Value,
		Secret:    req.Secret,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, saved)
}

func (h *VariablesHandler) ListProject(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	h.list(w, r, &project.ID)
}

func (h *VariablesHandler) ListGlobal(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, nil)
}

func (h *VariablesHandler) list(w http.ResponseWriter, r *http.Request, projectID *uuid.UUID) {
	variables, err := h.variables.List(r.Context(), projectID)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusOK, variables)
}

func (h *VariablesHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	h.delete(w, r, &project.ID, r.PathValue("key"))
}

func (h *VariablesHandler) DeleteGlobal(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, nil, r.PathValue("key"))
}

func (h *VariablesHandler) delete(w http.ResponseWriter, r *http.Request, projectID *uuid.UUID, key string) {
	if err := h.variables.Delete(r.Context(), projectID, key); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
