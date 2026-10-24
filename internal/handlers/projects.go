package handlers

import (
	"net/http"

	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

type ProjectsHandler struct {
	store     stores.Store
	workspace *workspace.Workspace
	projects  *projects.Module
}

func NewProjectsHandler(store stores.Store, ws *workspace.Workspace, projects *projects.Module) *ProjectsHandler {
	return &ProjectsHandler{store: store, workspace: ws, projects: projects}
}

type projectRequest struct {
	Name          string          `json:"name"`
	RepoURL       string          `json:"repo_url"`
	Provider      stores.Provider `json:"provider"`
	AuthType      stores.AuthType `json:"auth_type"`
	AuthUser      string          `json:"auth_user"`
	AuthSecret    string          `json:"auth_secret"`
	WebhookSecret string          `json:"webhook_secret"`
	DefaultBranch string          `json:"default_branch"`
}

func (h *ProjectsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req projectRequest
	if err := bind.JSON(r.Body, &req); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	project, err := h.projects.Create(r.Context(), stores.Project{
		Name:          req.Name,
		RepoURL:       req.RepoURL,
		Provider:      req.Provider,
		AuthType:      req.AuthType,
		AuthUser:      req.AuthUser,
		AuthSecret:    req.AuthSecret,
		WebhookSecret: req.WebhookSecret,
		DefaultBranch: req.DefaultBranch,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusCreated, project)
}

func (h *ProjectsHandler) List(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projects.List(r.Context())
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusOK, projects)
}

func (h *ProjectsHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, project)
}

func (h *ProjectsHandler) Update(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}

	var req projectRequest
	if err := bind.JSON(r.Body, &req); err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != "" {
		project.Name = req.Name
	}
	if req.RepoURL != "" {
		project.RepoURL = req.RepoURL
	}
	if req.Provider != "" {
		project.Provider = req.Provider
	}
	if req.AuthType != "" {
		project.AuthType = req.AuthType
	}
	if req.AuthUser != "" {
		project.AuthUser = req.AuthUser
	}
	if req.AuthSecret != "" {
		project.AuthSecret = req.AuthSecret
	}
	if req.WebhookSecret != "" {
		project.WebhookSecret = req.WebhookSecret
	}
	if req.DefaultBranch != "" {
		project.DefaultBranch = req.DefaultBranch
	}
	updated, err := h.projects.Update(r.Context(), project)
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, updated)
}

func (h *ProjectsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if err := h.projects.Delete(r.Context(), project.ID); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProjectsHandler) ListConfigs(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}

	dir := h.workspace.DiscoveryDir(project.ID)
	workflows, err := ci.DiscoverProjectWorkflows(r.Context(), project, dir, project.DefaultBranch)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err := ci.SyncProjectSchedules(r.Context(), h.store, project.ID, dir); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusOK, map[string]any{"project": project.Name, "workflows": workflows})
}
