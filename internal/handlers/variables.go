package handlers

import (
	"errors"
	"net/http"

	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type VariablesHandler struct {
	store stores.Store
}

func NewVariablesHandler(store stores.Store) *VariablesHandler {
	return &VariablesHandler{store: store}
}

type variableRequest struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

func (h *VariablesHandler) SetProject(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.set(w, r, project.ID)
}

func (h *VariablesHandler) SetGlobal(w http.ResponseWriter, r *http.Request) {
	h.set(w, r, "")
}

func (h *VariablesHandler) set(w http.ResponseWriter, r *http.Request, projectID string) {
	var req variableRequest
	if err := bind.JSON(r.Body, &req); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	if req.Key == "" {
		render.Error(w, http.StatusBadRequest, errors.New("key is required"))
		return
	}

	variable := stores.Variable{ProjectID: projectID, Key: req.Key, Value: req.Value, Secret: req.Secret}
	if err := h.store.SetVariable(r.Context(), variable); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusOK, maskVariable(variable))
}

func (h *VariablesHandler) ListProject(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.list(w, r, project.ID)
}

func (h *VariablesHandler) ListGlobal(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, "")
}

func (h *VariablesHandler) list(w http.ResponseWriter, r *http.Request, projectID string) {
	variables, err := h.store.ListVariables(r.Context(), projectID)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]stores.Variable, 0, len(variables))
	for _, v := range variables {
		out = append(out, maskVariable(v))
	}
	render.JSON(w, http.StatusOK, out)
}

func (h *VariablesHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.delete(w, r, project.ID, r.PathValue("key"))
}

func (h *VariablesHandler) DeleteGlobal(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, "", r.PathValue("key"))
}

func (h *VariablesHandler) delete(w http.ResponseWriter, r *http.Request, projectID, key string) {
	if err := h.store.DeleteVariable(r.Context(), projectID, key); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func maskVariable(v stores.Variable) stores.Variable {
	if v.Secret {
		v.Value = ""
	}
	return v
}
