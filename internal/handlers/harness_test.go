package handlers

import (
	"path/filepath"

	"github.com/Thiht/pici/internal/app/artifacts"
	"github.com/Thiht/pici/internal/app/cache"
	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/app/variables"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

// newTestHandler builds the JSON surface the way main does, from the same
// modules, with a workspace rooted at dir.
func newTestHandler(store stores.Store, runner *ci.Runner, engine *docker.Engine, dir string, maxSnapshotSize int64, svc *auth.Service) *Handler {
	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	var cacheEngine cache.Engine
	if engine != nil {
		cacheEngine = engine
	}
	return New(Deps{
		Store:           store,
		Runner:          runner,
		Engine:          engine,
		Workspace:       ws,
		Projects:        projects.New(store),
		Executions:      executions.New(store, ws, ws, runner),
		Artifacts:       artifacts.New(ws),
		Cache:           cache.New(cacheEngine),
		Variables:       variables.New(store),
		MaxSnapshotSize: maxSnapshotSize,
		Auth:            svc,
		BuildVersion:    "dev",
	})
}
