package middlewares

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/Thiht/pici/internal/handlers/render"
)

// SessionCookie carries the API token for browser sessions. It is set by the
// web UI login form so that native navigation and form submissions work.
const SessionCookie = "pici_token"

func Auth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(credential(r)), []byte(token)) != 1 {
			unauthorized(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isExempt lists the only paths reachable without a token: health checks,
// incoming webhooks, status badges, the login form, and the static assets it
// needs.
func isExempt(path string) bool {
	return path == "/health" ||
		path == "/login" ||
		strings.HasPrefix(path, "/badge/") ||
		strings.HasPrefix(path, "/api/webhooks/") ||
		strings.HasPrefix(path, "/static/")
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		render.Error(w, http.StatusUnauthorized, errors.New("invalid or missing API token"))
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func credential(r *http.Request) string {
	if token := bearerToken(r); token != "" {
		return token
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("X-API-Token"); h != "" {
		return h
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}
