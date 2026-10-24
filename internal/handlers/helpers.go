package handlers

import (
	"errors"
	"net/http"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/handlers/render"
	"github.com/Thiht/pici/internal/stores"
)

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrNotFound), errors.Is(err, stores.ErrNotFound):
		render.Error(w, http.StatusNotFound, err)
	case errors.Is(err, app.ErrInvalid):
		render.Error(w, http.StatusBadRequest, err)
	default:
		render.Error(w, http.StatusInternalServerError, err)
	}
}
