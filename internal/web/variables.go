package web

import (
	"net/http"
	"strings"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
)

type variablesPage struct {
	base
	Variables []stores.Variable
}

func (h *Handler) GlobalVariables(w http.ResponseWriter, r *http.Request) {
	variables, err := h.store.ListVariables(r.Context(), nil)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data := variablesPage{base: h.base(w, r, "Variables", "variables"), Variables: maskVariables(variables)}
	h.render(w, r, http.StatusOK, "variables_list", data)
}

func (h *Handler) ProjectVariables(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	variables, err := h.store.ListVariables(r.Context(), &project.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data := projectVariablesPage{base: h.base(w, r, project.Name+" variables", "projects"), Project: project, Variables: maskVariables(variables)}
	h.render(w, r, http.StatusOK, "project_variables", data)
}

func (h *Handler) ProjectVariableSet(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	h.setVariable(w, r, &project.ID, "/projects/"+project.ID.String()+"/variables")
}

func (h *Handler) GlobalVariableSet(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	h.setVariable(w, r, nil, "/variables")
}

func (h *Handler) setVariable(w http.ResponseWriter, r *http.Request, projectID *uuid.UUID, back string) {
	key := strings.TrimSpace(r.FormValue("key"))
	if key == "" {
		setFlash(w, "error", "Key is required.")
		redirect(w, r, back)
		return
	}
	variable := stores.Variable{
		ProjectID: projectID,
		Key:       key,
		Value:     r.FormValue("value"),
		Secret:    r.FormValue("secret") != "",
	}
	if err := h.store.SetVariable(r.Context(), variable); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Variable saved.")
	redirect(w, r, back)
}

func (h *Handler) ProjectVariableDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	if err := h.store.DeleteVariable(r.Context(), &project.ID, r.PathValue("key")); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Variable deleted.")
	redirect(w, r, "/projects/"+project.ID.String()+"/variables")
}

func (h *Handler) GlobalVariableDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	if err := h.store.DeleteVariable(r.Context(), nil, r.PathValue("key")); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Variable deleted.")
	redirect(w, r, "/variables")
}
