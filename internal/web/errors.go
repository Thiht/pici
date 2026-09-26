package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Thiht/pici/internal/stores"
)

type errorPage struct {
	base
	Message string
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	data := errorPage{base: h.base(w, r, "Not found", ""), Message: "This page does not exist."}
	h.render(w, r, http.StatusNotFound, "error", data)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("web request failed", "path", r.URL.Path, "error", err)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Retarget", "#flash")
		w.Header().Set("HX-Reswap", "innerHTML")
		h.render(w, r, http.StatusInternalServerError, "flash", base{FlashKind: "error", FlashText: "Something went wrong."})
		return
	}
	data := errorPage{base: h.base(w, r, "Error", ""), Message: "Something went wrong."}
	h.render(w, r, http.StatusInternalServerError, "error", data)
}

func (h *Handler) storeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, stores.ErrNotFound) {
		h.notFound(w, r)
		return
	}
	h.serverError(w, r, err)
}

func (h *Handler) csrfError(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "invalid CSRF token", http.StatusForbidden)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
