package handlers

import (
	"net/http"

	"github.com/Thiht/pici/internal/app/artifacts"
	"github.com/Thiht/pici/internal/app/cache"
	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/app/variables"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/handlers/middlewares"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

type Handler struct {
	projects   *ProjectsHandler
	variables  *VariablesHandler
	executions *ExecutionsHandler
	webhooks   *WebhooksHandler
	artifacts  *ArtifactsHandler
	system     *SystemHandler
	cache      *CacheHandler

	auth *auth.Service
}

type Deps struct {
	Store           stores.Store
	Runner          *ci.Runner
	Engine          *docker.Engine
	Workspace       *workspace.Workspace
	Projects        *projects.Module
	Executions      *executions.Module
	Artifacts       *artifacts.Module
	Cache           *cache.Module
	Variables       *variables.Module
	MaxSnapshotSize int64
	Auth            *auth.Service
	BuildVersion    string
}

func New(d Deps) *Handler {
	return &Handler{
		projects:  NewProjectsHandler(d.Store, d.Workspace, d.Projects),
		variables: NewVariablesHandler(d.Projects, d.Variables),
		executions: NewExecutionsHandler(ExecutionsDeps{
			Store:           d.Store,
			Runner:          d.Runner,
			MaxSnapshotSize: d.MaxSnapshotSize,
			Projects:        d.Projects,
			Executions:      d.Executions,
		}),
		webhooks:  NewWebhooksHandler(d.Store, d.Runner, d.Workspace, d.Projects),
		artifacts: NewArtifactsHandler(d.Projects, d.Executions, d.Artifacts),
		system:    NewSystemHandler(d.Store, d.Engine, d.BuildVersion),
		cache:     NewCacheHandler(d.Cache, d.Projects),
		auth:      d.Auth,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", h.system.Health)
	mux.HandleFunc("GET /version", h.system.Version)
	mux.HandleFunc("GET /badge/{id}", h.projects.Badge)

	mux.HandleFunc("POST /api/projects", h.projects.Create)
	mux.HandleFunc("GET /api/projects", h.projects.List)
	mux.HandleFunc("GET /api/projects/{id}", h.projects.Get)
	mux.HandleFunc("PUT /api/projects/{id}", h.projects.Update)
	mux.HandleFunc("DELETE /api/projects/{id}", h.projects.Delete)
	mux.HandleFunc("GET /api/projects/{id}/configs", h.projects.ListConfigs)

	mux.HandleFunc("GET /api/projects/{id}/cache", h.cache.Get)
	mux.HandleFunc("DELETE /api/projects/{id}/cache/images", h.cache.DeleteImage)
	mux.HandleFunc("DELETE /api/projects/{id}/cache/volumes", h.cache.DeleteVolume)
	mux.HandleFunc("GET /api/cache/registry", h.cache.Registry)
	mux.HandleFunc("DELETE /api/cache/registry", h.cache.PurgeRegistry)

	mux.HandleFunc("POST /api/projects/{id}/variables", h.variables.SetProject)
	mux.HandleFunc("GET /api/projects/{id}/variables", h.variables.ListProject)
	mux.HandleFunc("DELETE /api/projects/{id}/variables/{key}", h.variables.DeleteProject)
	mux.HandleFunc("POST /api/variables", h.variables.SetGlobal)
	mux.HandleFunc("GET /api/variables", h.variables.ListGlobal)
	mux.HandleFunc("DELETE /api/variables/{key}", h.variables.DeleteGlobal)

	mux.HandleFunc("POST /api/projects/{id}/executions", h.executions.Create)
	mux.HandleFunc("GET /api/projects/{id}/executions", h.executions.List)
	mux.HandleFunc("GET /api/projects/{id}/executions/{executionID}", h.executions.Get)
	mux.HandleFunc("GET /api/projects/{id}/executions/{executionID}/attempts", h.executions.ListAttempts)
	mux.HandleFunc("GET /api/projects/{id}/executions/{executionID}/logs", h.executions.Logs)
	mux.HandleFunc("GET /api/projects/{id}/executions/{executionID}/logs/stream", h.executions.LogStream)
	mux.HandleFunc("GET /api/projects/{id}/executions/{executionID}/steps/{step}/logs", h.executions.StepLogs)
	mux.HandleFunc("POST /api/projects/{id}/executions/{executionID}/cancel", h.executions.Cancel)
	mux.HandleFunc("POST /api/projects/{id}/executions/{executionID}/rebuild", h.executions.Rebuild)
	mux.HandleFunc("POST /api/projects/{id}/executions/{executionID}/retry", h.executions.Retry)
	mux.HandleFunc("GET /api/projects/{id}/executions/{executionID}/artifacts", h.artifacts.List)
	mux.HandleFunc("GET /api/projects/{id}/executions/{executionID}/artifacts/{step}/{path...}", h.artifacts.Download)

	mux.HandleFunc("POST /api/validate", h.system.Validate)

	mux.HandleFunc("POST /api/webhooks/github/{id}", h.webhooks.GitHub)
	mux.HandleFunc("POST /api/webhooks/gitlab/{id}", h.webhooks.GitLab)

	return middlewares.Log(middlewares.API(h.auth, mux))
}
