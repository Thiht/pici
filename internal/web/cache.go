package web

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

type cacheImage struct {
	Tag        string
	Size       int64
	Created    time.Time
	Containers int64
}

type cacheVolume struct {
	Cache   string
	Name    string
	Size    int64
	Created time.Time
}

type projectCachePage struct {
	base
	Project     stores.Project
	Images      []cacheImage
	Volumes     []cacheVolume
	ImagesSize  int64
	VolumesSize int64
	TotalSize   int64
	Unavailable bool
}

func (h *Handler) ProjectCache(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	data := projectCachePage{base: h.base(w, r, project.Name+" cache", "projects"), Project: project}

	if h.runner == nil || h.runner.Engine == nil {
		data.Unavailable = true
		h.render(w, r, http.StatusOK, "project_cache", data)
		return
	}

	imagePrefix := ci.ImagePrefix(project.ID.String())
	images, err := h.runner.Engine.Images(r.Context(), imagePrefix+"*")
	if err != nil {
		data.Unavailable = true
		h.render(w, r, http.StatusOK, "project_cache", data)
		return
	}
	for _, img := range images {
		var tag string
		for _, t := range img.Tags {
			if strings.HasPrefix(t, imagePrefix) {
				tag = t
				break
			}
		}
		if tag == "" {
			continue
		}
		data.Images = append(data.Images, cacheImage{Tag: tag, Size: img.Size, Created: img.Created, Containers: img.Containers})
		data.ImagesSize += img.Size
	}
	sort.Slice(data.Images, func(i, j int) bool { return data.Images[i].Tag < data.Images[j].Tag })

	volumePrefix := ci.VolumePrefix(project.ID.String())
	volumes, err := h.runner.Engine.Volumes(r.Context(), volumePrefix)
	if err != nil {
		data.Unavailable = true
		h.render(w, r, http.StatusOK, "project_cache", data)
		return
	}
	for _, v := range volumes {
		data.Volumes = append(data.Volumes, cacheVolume{
			Cache:   strings.TrimPrefix(v.Name, volumePrefix),
			Name:    v.Name,
			Size:    v.Size,
			Created: v.Created,
		})
		data.VolumesSize += v.Size
	}
	sort.Slice(data.Volumes, func(i, j int) bool { return data.Volumes[i].Cache < data.Volumes[j].Cache })

	data.TotalSize = data.ImagesSize + data.VolumesSize
	h.render(w, r, http.StatusOK, "project_cache", data)
}

func (h *Handler) ProjectCacheImageDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	back := "/projects/" + project.ID.String() + "/cache"
	reference := strings.TrimSpace(r.FormValue("reference"))
	if !strings.HasPrefix(reference, ci.ImagePrefix(project.ID.String())) {
		setFlash(w, "error", "Unknown image.")
		redirect(w, r, back)
		return
	}
	if h.runner == nil || h.runner.Engine == nil {
		setFlash(w, "error", "Docker is unavailable.")
		redirect(w, r, back)
		return
	}
	if err := h.runner.Engine.RemoveImage(r.Context(), reference); err != nil {
		setFlash(w, "error", "Could not delete image: "+err.Error())
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
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	back := "/projects/" + project.ID.String() + "/cache"
	name := strings.TrimSpace(r.FormValue("name"))
	if !strings.HasPrefix(name, ci.VolumePrefix(project.ID.String())) {
		setFlash(w, "error", "Unknown cache volume.")
		redirect(w, r, back)
		return
	}
	if h.runner == nil || h.runner.Engine == nil {
		setFlash(w, "error", "Docker is unavailable.")
		redirect(w, r, back)
		return
	}
	if err := h.runner.Engine.RemoveVolume(r.Context(), name); err != nil {
		setFlash(w, "error", "Could not delete cache volume: "+err.Error())
		redirect(w, r, back)
		return
	}
	setFlash(w, "success", "Cache volume deleted.")
	redirect(w, r, back)
}
