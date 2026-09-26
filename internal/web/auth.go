package web

import (
	"crypto/subtle"
	"net/http"

	"github.com/Thiht/pici/internal/handlers/middlewares"
)

type loginPage struct {
	base
	Error string
	Next  string
}

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.apiToken == "" {
		redirect(w, r, "/")
		return
	}
	if c, err := r.Cookie(middlewares.SessionCookie); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(h.apiToken)) == 1 {
		redirect(w, r, "/")
		return
	}
	data := loginPage{base: h.base(w, r, "Sign in", ""), Next: safeNext(r.URL.Query().Get("next"))}
	h.render(w, r, http.StatusOK, "login", data)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.FormValue("next"))
	if h.apiToken == "" {
		redirect(w, r, "/")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.FormValue("token")), []byte(h.apiToken)) != 1 {
		data := loginPage{base: h.base(w, r, "Sign in", ""), Error: "Invalid token.", Next: next}
		h.render(w, r, http.StatusUnauthorized, "login", data)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middlewares.SessionCookie,
		Value:    h.apiToken,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
	if next == "" {
		next = "/"
	}
	redirect(w, r, next)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: middlewares.SessionCookie, Value: "", Path: "/", MaxAge: -1})
	redirect(w, r, "/login")
}

// safeNext only allows local, absolute paths so a crafted ?next= cannot
// redirect the user off-site after login.
func safeNext(next string) string {
	if next == "" || next[0] != '/' || len(next) > 1 && next[1] == '/' {
		return ""
	}
	return next
}
