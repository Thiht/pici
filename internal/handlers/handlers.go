package handlers

import (
	"net/http"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/handlers/middlewares"
	"github.com/Thiht/pici/internal/stores"
)

type Handler struct {
	projects   *ProjectsHandler
	variables  *VariablesHandler
	executions *ExecutionsHandler
	webhooks   *WebhooksHandler
	artifacts  *ArtifactsHandler
	system     *SystemHandler

	apiToken string
}

func New(store stores.Store, runner *ci.Runner, engine *docker.Engine, workspaceDir, mountPath, apiToken, buildVersion string) *Handler {
	return &Handler{
		projects:   NewProjectsHandler(store, runner, workspaceDir),
		variables:  NewVariablesHandler(store),
		executions: NewExecutionsHandler(store, runner),
		webhooks:   NewWebhooksHandler(store, runner, workspaceDir),
		artifacts:  NewArtifactsHandler(store, runner),
		system:     NewSystemHandler(store, engine, buildVersion),
		apiToken:   apiToken,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", h.system.Health)
	mux.HandleFunc("GET /version", h.system.Version)

	mux.HandleFunc("POST /api/projects", h.projects.Create)
	mux.HandleFunc("GET /api/projects", h.projects.List)
	mux.HandleFunc("GET /api/projects/{id}", h.projects.Get)
	mux.HandleFunc("PUT /api/projects/{id}", h.projects.Update)
	mux.HandleFunc("DELETE /api/projects/{id}", h.projects.Delete)
	mux.HandleFunc("GET /api/projects/{id}/configs", h.projects.ListConfigs)

	mux.HandleFunc("POST /api/projects/{id}/variables", h.variables.SetProject)
	mux.HandleFunc("GET /api/projects/{id}/variables", h.variables.ListProject)
	mux.HandleFunc("DELETE /api/projects/{id}/variables/{key}", h.variables.DeleteProject)
	mux.HandleFunc("POST /api/variables", h.variables.SetGlobal)
	mux.HandleFunc("GET /api/variables", h.variables.ListGlobal)
	mux.HandleFunc("DELETE /api/variables/{key}", h.variables.DeleteGlobal)

	mux.HandleFunc("POST /api/projects/{id}/executions", h.executions.Create)
	mux.HandleFunc("GET /api/projects/{id}/executions", h.executions.List)
	mux.HandleFunc("GET /api/executions/{id}", h.executions.Get)
	mux.HandleFunc("GET /api/executions/{id}/logs", h.executions.Logs)
	mux.HandleFunc("GET /api/executions/{id}/logs/stream", h.executions.LogStream)
	mux.HandleFunc("GET /api/executions/{id}/steps/{step}/logs", h.executions.StepLogs)
	mux.HandleFunc("POST /api/executions/{id}/cancel", h.executions.Cancel)
	mux.HandleFunc("POST /api/executions/{id}/rebuild", h.executions.Rebuild)
	mux.HandleFunc("GET /api/executions/{id}/artifacts", h.artifacts.List)
	mux.HandleFunc("GET /api/executions/{id}/artifacts/{step}/{path...}", h.artifacts.Download)

	mux.HandleFunc("POST /api/validate", h.system.Validate)

	mux.HandleFunc("POST /api/webhooks/github/{id}", h.webhooks.GitHub)
	mux.HandleFunc("POST /api/webhooks/gitlab/{id}", h.webhooks.GitLab)

	return middlewares.Log(middlewares.Auth(h.apiToken, mux))
}
