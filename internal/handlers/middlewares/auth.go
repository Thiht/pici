package middlewares

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/handlers/render"
)

// API authenticates JSON API requests with a personal API token. Browser
// sessions are intentionally not accepted on the API: the web UI talks to the
// store directly, and keeping cookies out of the API avoids CSRF there.
func API(svc *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		user, err := svc.AuthenticateToken(r.Context(), bearerToken(r))
		if err != nil {
			if !errors.Is(err, auth.ErrUnauthorized) {
				render.Error(w, http.StatusInternalServerError, err)
				return
			}
			unauthorized(w, r)
			return
		}
		user.OAuthToken = ""
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{User: user})))
	})
}

// isExempt lists the only API paths reachable without a token: health checks,
// incoming webhooks, and public status badges.
func isExempt(path string) bool {
	return path == "/health" ||
		strings.HasPrefix(path, "/badge/") ||
		strings.HasPrefix(path, "/api/webhooks/")
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		render.Error(w, http.StatusUnauthorized, errors.New("invalid or missing API token"))
		return
	}
	http.Error(w, "invalid or missing API token", http.StatusUnauthorized)
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
