package handlers

import (
	"net/http"

	"github.com/Thiht/pici/internal/app/cache"
	"github.com/Thiht/pici/internal/app/projects"
	"github.com/Thiht/pici/internal/handlers/render"
)

type CacheHandler struct {
	cache    *cache.Module
	projects *projects.Module
}

func NewCacheHandler(cache *cache.Module, projects *projects.Module) *CacheHandler {
	return &CacheHandler{cache: cache, projects: projects}
}

type cacheResponse struct {
	Project   string         `json:"project"`
	Images    []cache.Image  `json:"images"`
	Volumes   []cache.Volume `json:"volumes"`
	TotalSize int64          `json:"total_size"`
}

func (h *CacheHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	summary, err := h.cache.Get(r.Context(), project)
	if err != nil {
		storeError(w, err)
		return
	}
	render.JSON(w, http.StatusOK, cacheResponse{
		Project:   project.Name,
		Images:    summary.Images,
		Volumes:   summary.Volumes,
		TotalSize: summary.TotalSize,
	})
}

func (h *CacheHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if err := h.cache.RemoveImage(r.Context(), project, r.URL.Query().Get("reference")); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CacheHandler) DeleteVolume(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if err := h.cache.RemoveVolume(r.Context(), project, r.URL.Query().Get("name")); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
