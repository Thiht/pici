package handlers

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type CacheHandler struct {
	store  stores.Store
	engine *docker.Engine
}

func NewCacheHandler(store stores.Store, engine *docker.Engine) *CacheHandler {
	return &CacheHandler{store: store, engine: engine}
}

type cacheImageResponse struct {
	Reference  string    `json:"reference"`
	Size       int64     `json:"size"`
	Created    time.Time `json:"created"`
	Containers int64     `json:"containers"`
}

type cacheVolumeResponse struct {
	Name    string    `json:"name"`
	Cache   string    `json:"cache"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`
}

type cacheResponse struct {
	Project   string                `json:"project"`
	Images    []cacheImageResponse  `json:"images"`
	Volumes   []cacheVolumeResponse `json:"volumes"`
	TotalSize int64                 `json:"total_size"`
}

func (h *CacheHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	if h.engine == nil {
		render.Error(w, http.StatusServiceUnavailable, errors.New("docker is unavailable"))
		return
	}

	imagePrefix := ci.ImagePrefix(project.ID.String())
	images, err := h.engine.Images(r.Context(), imagePrefix+"*")
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	resp := cacheResponse{Project: project.Name, Images: []cacheImageResponse{}, Volumes: []cacheVolumeResponse{}}
	for _, img := range images {
		var reference string
		for _, tag := range img.Tags {
			if strings.HasPrefix(tag, imagePrefix) {
				reference = tag
				break
			}
		}
		if reference == "" {
			continue
		}
		resp.Images = append(resp.Images, cacheImageResponse{Reference: reference, Size: img.Size, Created: img.Created, Containers: img.Containers})
		resp.TotalSize += img.Size
	}
	sort.Slice(resp.Images, func(i, j int) bool { return resp.Images[i].Reference < resp.Images[j].Reference })

	volumePrefix := ci.VolumePrefix(project.ID.String())
	volumes, err := h.engine.Volumes(r.Context(), volumePrefix)
	if err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	for _, v := range volumes {
		resp.Volumes = append(resp.Volumes, cacheVolumeResponse{
			Name:    v.Name,
			Cache:   strings.TrimPrefix(v.Name, volumePrefix),
			Size:    v.Size,
			Created: v.Created,
		})
		resp.TotalSize += v.Size
	}
	sort.Slice(resp.Volumes, func(i, j int) bool { return resp.Volumes[i].Cache < resp.Volumes[j].Cache })

	render.JSON(w, http.StatusOK, resp)
}

func (h *CacheHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	reference := strings.TrimSpace(r.URL.Query().Get("reference"))
	if !strings.HasPrefix(reference, ci.ImagePrefix(project.ID.String())) {
		render.Error(w, http.StatusBadRequest, errors.New("unknown image"))
		return
	}
	if h.engine == nil {
		render.Error(w, http.StatusServiceUnavailable, errors.New("docker is unavailable"))
		return
	}
	if err := h.engine.RemoveImage(r.Context(), reference); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *CacheHandler) DeleteVolume(w http.ResponseWriter, r *http.Request) {
	project, err := resolveProject(r.Context(), h.store, r.PathValue("id"))
	if err != nil {
		storeError(w, err)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if !strings.HasPrefix(name, ci.VolumePrefix(project.ID.String())) {
		render.Error(w, http.StatusBadRequest, errors.New("unknown cache volume"))
		return
	}
	if h.engine == nil {
		render.Error(w, http.StatusServiceUnavailable, errors.New("docker is unavailable"))
		return
	}
	if err := h.engine.RemoveVolume(r.Context(), name); err != nil {
		render.Error(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
