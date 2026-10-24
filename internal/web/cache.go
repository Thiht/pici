package web

import (
	"errors"
	"net/http"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/app/cache"
	"github.com/Thiht/pici/internal/stores"
)

type projectCachePage struct {
	base
	Project     stores.Project
	Images      []cache.Image
	Volumes     []cache.Volume
	ImagesSize  int64
	VolumesSize int64
	TotalSize   int64
	Unavailable bool
}

func (h *Handler) ProjectCache(w http.ResponseWriter, r *http.Request) {
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	data := projectCachePage{base: h.base(w, r, project.Name+" cache", "projects"), Project: project}
	summary, err := h.cache.Get(r.Context(), project)
	if errors.Is(err, app.ErrUnavailable) {
		data.Unavailable = true
		h.render(w, r, http.StatusOK, "project_cache", data)
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data.Images = summary.Images
	data.Volumes = summary.Volumes
	data.ImagesSize = summary.ImagesSize
	data.VolumesSize = summary.VolumesSize
	data.TotalSize = summary.TotalSize
	h.render(w, r, http.StatusOK, "project_cache", data)
}

func (h *Handler) ProjectCacheImageDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	back := "/projects/" + project.ID.String() + "/cache"
	if err := h.cache.RemoveImage(r.Context(), project, r.FormValue("reference")); err != nil {
		switch {
		case errors.Is(err, app.ErrInvalid):
			setFlash(w, "error", "Unknown image.")
		case errors.Is(err, app.ErrUnavailable):
			setFlash(w, "error", "Docker is unavailable.")
		default:
			setFlash(w, "error", "Could not delete image: "+err.Error())
		}
		redirect(w, r, back)
		return
	}
	setFlash(w, "success", "Image deleted.")
	redirect(w, r, back)
}

func (h *Handler) ProjectCacheVolumeDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := h.projects.Resolve(r.Context(), r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	back := "/projects/" + project.ID.String() + "/cache"
	if err := h.cache.RemoveVolume(r.Context(), project, r.FormValue("name")); err != nil {
		switch {
		case errors.Is(err, app.ErrInvalid):
			setFlash(w, "error", "Unknown cache volume.")
		case errors.Is(err, app.ErrUnavailable):
			setFlash(w, "error", "Docker is unavailable.")
		default:
			setFlash(w, "error", "Could not delete cache volume: "+err.Error())
		}
		redirect(w, r, back)
		return
	}
	setFlash(w, "success", "Cache volume deleted.")
	redirect(w, r, back)
}
