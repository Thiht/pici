package web

import (
	"path/filepath"

	"github.com/Thiht/pici/internal/app/artifacts"
	"github.com/Thiht/pici/internal/app/cache"
	"github.com/Thiht/pici/internal/app/executions"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/app/variables"
	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

func newTestHandler(store stores.Store, runner *ci.Runner, dir string, svc *auth.Service, publicBaseURL, buildVersion string) *Handler {
	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	return New(Deps{
		Store:         store,
		Runner:        runner,
		Workspace:     ws,
		Projects:      projects.New(store),
		Executions:    executions.New(store, ws, ws, runner),
		Artifacts:     artifacts.New(ws),
		Cache:         cache.New(nil),
		Variables:     variables.New(store),
		Auth:          svc,
		PublicBaseURL: publicBaseURL,
		BuildVersion:  buildVersion,
	})
}
