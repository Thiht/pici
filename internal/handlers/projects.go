package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/handlers/bind"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type ProjectsHandler struct {
	store        stores.Store
	runner       *ci.Runner
	workspaceDir string
}

func NewProjectsHandler(store stores.Store, runner *ci.Runner, workspaceDir string) *ProjectsHandler {
	return &ProjectsHandler{store: store, runner: runner, workspaceDir: workspaceDir}
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
	if req.Name == "" {
		render.Error(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	if req.RepoURL == "" {
		render.Error(w, http.StatusBadRequest, errors.New("repo_url is required"))
		return
	}
	if req.Provider == "" {
		switch {
		case strings.Contains(req.RepoURL, "github.com"):
			req.Provider = stores.ProviderGithub
		case strings.Contains(req.RepoURL, "gitlab"):
			req.Provider = stores.ProviderGitlab
		default:
			req.Provider = stores.ProviderGeneric
		}
	}
	if req.AuthType == "" {
		if req.AuthSecret != "" {
			req.AuthType = stores.AuthTypeToken
		} else {
			req.AuthType = stores.AuthTypeNone
		}
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = detectDefaultBranch(r.Context(), req.RepoURL, req.AuthType, req.AuthUser, req.AuthSecret)
	}

	now := time.Now()
	project, err := h.store.CreateProject(r.Context(), stores.Project{
		ID:            uuid.New(),
		Name:          req.Name,
		RepoURL:       req.RepoURL,
		Provider:      req.Provider,
		AuthType:      req.AuthType,
		AuthUser:      req.AuthUser,
		AuthSecret:    req.AuthSecret,
		WebhookSecret: req.WebhookSecret,
		DefaultBranch: req.DefaultBranch,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusCreated, project)
}

func (h *ProjectsHandler) List(w http.ResponseWriter, r *http.Request) {
	projects, err := h.store.ListProjects(r.Context())
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusOK, projects)
}

func (h *ProjectsHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, project)
}

func (h *ProjectsHandler) Update(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
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
	if project.DefaultBranch == "" {
		project.DefaultBranch = detectDefaultBranch(r.Context(), project.RepoURL, project.AuthType, project.AuthUser, project.AuthSecret)
	}
	project.UpdatedAt = time.Now()

	updated, err := h.store.UpdateProject(r.Context(), project)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	render.JSON(w, http.StatusOK, updated)
}

func (h *ProjectsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if err := h.store.DeleteProject(r.Context(), project.ID); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func detectDefaultBranch(ctx context.Context, repoURL string, authType stores.AuthType, authUser, authSecret string) string {
	refs, err := git.ListRefs(ctx, repoURL, git.Auth{
		Type:   authType.String(),
		User:   authUser,
		Secret: authSecret,
	})
	if err != nil {
		return ""
	}
	return refs.Head
}

func (h *ProjectsHandler) ListConfigs(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}

	dir := h.workspaceDir + "/" + project.ID.String() + "/_discovery"
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
