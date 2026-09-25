package middlewares

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/Thiht/pici/internal/handlers/render"
)

func Auth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/api/webhooks/") {
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(bearerToken(r)), []byte(token)) != 1 {
			render.Error(w, http.StatusUnauthorized, errors.New("invalid or missing API token"))
			return
		}
		next.ServeHTTP(w, r)
	})
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
