package handlers

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"uuid"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

type ArtifactsHandler struct {
	store  stores.Store
	runner *ci.Runner
}

func NewArtifactsHandler(store stores.Store, runner *ci.Runner) *ArtifactsHandler {
	return &ArtifactsHandler{store: store, runner: runner}
}

type artifact struct {
	Step string `json:"step"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (h *ArtifactsHandler) List(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	execution, err := h.store.GetExecution(r.Context(), id)
	if err != nil {
		if errors.Is(err, stores.ErrNotFound) {
			render.Error(w, http.StatusNotFound, err)
		} else {
			render.Error(w, http.StatusInternalServerError, err)
		}
		return
	}

	dir := h.runner.ArtifactDir(execution.ID.String())
	out := []artifact{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) != 2 {
			return nil
		}
		info, _ := d.Info()
		var size int64
		if info != nil {
			size = info.Size()
		}
		out = append(out, artifact{Step: stepNameForIndex(execution, parts[0]), Path: filepath.ToSlash(parts[1]), Size: size})
		return nil
	})
	render.JSON(w, http.StatusOK, out)
}

func (h *ArtifactsHandler) Download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	step := r.PathValue("step")
	rel := r.PathValue("path")

	execID, err := uuid.Parse(id)
	if err != nil {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	execution, err := h.store.GetExecution(r.Context(), execID)
	if err != nil {
		if errors.Is(err, stores.ErrNotFound) {
			render.Error(w, http.StatusNotFound, err)
		} else {
			render.Error(w, http.StatusInternalServerError, err)
		}
		return
	}

	index := -1
	for i, sr := range execution.Steps {
		if sr.Name == step {
			index = i
			break
		}
	}
	if index < 0 {
		render.Error(w, http.StatusNotFound, errors.New("step not found"))
		return
	}

	dir := filepath.Join(h.runner.ArtifactDir(id), fmt.Sprintf("%03d", index))
	full := filepath.Clean(filepath.Join(dir, filepath.FromSlash(rel)))
	if full != dir && !strings.HasPrefix(full, dir+string(filepath.Separator)) {
		render.Error(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}

	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		render.Error(w, http.StatusNotFound, errors.New("artifact not found"))
		return
	}
	http.ServeFile(w, r, full)
}

func stepNameForIndex(execution stores.Execution, indexDir string) string {
	idx, err := strconv.Atoi(indexDir)
	if err == nil && idx >= 0 && idx < len(execution.Steps) {
		return execution.Steps[idx].Name
	}
	return indexDir
}
