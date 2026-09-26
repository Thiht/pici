package handlers

import (
	"context"
	"errors"
	"net/http"
	"uuid"

	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

func resolveProject(ctx context.Context, store stores.Store, idOrName string) (stores.Project, error) {
	if id, err := uuid.Parse(idOrName); err == nil {
		if p, err := store.GetProject(ctx, id); err == nil {
			return p, nil
		} else if !errors.Is(err, stores.ErrNotFound) {
			return stores.Project{}, err
		}
	}
	return store.GetProjectByName(ctx, idOrName)
}

func storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, stores.ErrNotFound) {
		render.Error(w, http.StatusNotFound, err)
		return
	}
	render.Error(w, http.StatusInternalServerError, err)
}
