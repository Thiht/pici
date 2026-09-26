package handlers

import (
	"io"
	"net/http"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/version"
)

type SystemHandler struct {
	store        stores.Store
	engine       *docker.Engine
	buildVersion string
}

func NewSystemHandler(store stores.Store, engine *docker.Engine, buildVersion string) *SystemHandler {
	return &SystemHandler{store: store, engine: engine, buildVersion: buildVersion}
}

func (h *SystemHandler) Version(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, http.StatusOK, version.Get(h.buildVersion))
}

func (h *SystemHandler) Health(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"status": "ok"}
	if h.engine != nil {
		if err := h.engine.Ping(r.Context()); err != nil {
			render.JSON(w, http.StatusServiceUnavailable, map[string]any{"status": "ok", "docker": "unavailable"})
			return
		}
		resp["docker"] = "ok"
	}
	if n, err := h.store.CountPendingExecutions(r.Context()); err == nil {
		resp["queue"] = n
	}
	render.JSON(w, http.StatusOK, resp)
}

func (h *SystemHandler) Validate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		render.Error(w, http.StatusBadRequest, err)
		return
	}
	cfg, err := ci.Parse(body)
	if err != nil {
		render.JSON(w, http.StatusUnprocessableEntity, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	render.JSON(w, http.StatusOK, map[string]any{
		"valid":    true,
		"name":     cfg.Name,
		"steps":    len(cfg.Steps),
		"schedule": cfg.Schedule,
		"tags":     cfg.Tags,
		"branches": cfg.Branches,
		"paths":    cfg.Paths,
		"image":    cfg.Image,
	})
}
