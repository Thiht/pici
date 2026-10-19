package web

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/stores"
)

const defaultTokenLifetime = 90 * 24 * time.Hour

type tokenView struct {
	stores.APIToken
	Expired bool
}

type sessionView struct {
	stores.Session
	Current bool
}

type accountPage struct {
	base
	Tokens       []tokenView
	Sessions     []sessionView
	NewToken     string
	NewTokenName string
}

func (h *Handler) AccountPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.accountData(w, r, "", "")
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "account", data)
}

func (h *Handler) accountData(w http.ResponseWriter, r *http.Request, newToken, newTokenName string) (accountPage, error) {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return accountPage{}, auth.ErrUnauthorized
	}
	tokens, err := h.auth.ListAPITokens(r.Context(), principal.User.ID)
	if err != nil {
		return accountPage{}, err
	}
	sessions, err := h.auth.ListSessions(r.Context(), principal.User.ID)
	if err != nil {
		return accountPage{}, err
	}
	now := time.Now()
	tokenViews := make([]tokenView, 0, len(tokens))
	for _, token := range tokens {
		tokenViews = append(tokenViews, tokenView{
			APIToken: token,
			Expired:  token.ExpiresAt != nil && now.After(*token.ExpiresAt),
		})
	}
	sessionViews := make([]sessionView, 0, len(sessions))
	for _, session := range sessions {
		sessionViews = append(sessionViews, sessionView{
			Session: session,
			Current: principal.Session != nil && session.ID == principal.Session.ID,
		})
	}
	return accountPage{
		base:         h.base(w, r, "Account", ""),
		Tokens:       tokenViews,
		Sessions:     sessionViews,
		NewToken:     newToken,
		NewTokenName: newTokenName,
	}, nil
}

func (h *Handler) AccountTokenCreate(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		h.redirectToLogin(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Personal token"
	}
	if runes := []rune(name); len(runes) > 100 {
		name = string(runes[:100])
	}
	expiresAt, err := tokenExpiry(r.FormValue("expires"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw, _, err := h.auth.CreateAPIToken(r.Context(), principal.User.ID, name, expiresAt)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data, err := h.accountData(w, r, raw, name)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "account", data)
}

func (h *Handler) AccountTokenRename(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		h.redirectToLogin(w, r)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.notFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "token name is required", http.StatusBadRequest)
		return
	}
	if runes := []rune(name); len(runes) > 100 {
		name = string(runes[:100])
	}
	if err := h.auth.RenameAPIToken(r.Context(), principal.User.ID, id, name); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Token renamed.")
	redirect(w, r, "/account")
}

func (h *Handler) AccountTokenDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		h.redirectToLogin(w, r)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.notFound(w, r)
		return
	}
	if err := h.auth.DeleteAPIToken(r.Context(), principal.User.ID, id); err != nil {
		h.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Token revoked.")
	redirect(w, r, "/account")
}

func (h *Handler) AccountSessionDelete(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		h.redirectToLogin(w, r)
		return
	}
	id := r.PathValue("id")
	current := principal.Session != nil && principal.Session.ID == id
	if err := h.auth.DeleteSessionForUser(r.Context(), principal.User.ID, id); err != nil {
		h.serverError(w, r, err)
		return
	}
	if current {
		h.clearSessionCookie(w)
		redirect(w, r, "/login")
		return
	}
	setFlash(w, "success", "Session revoked.")
	redirect(w, r, "/account")
}

func (h *Handler) AccountSessionsDeleteAll(w http.ResponseWriter, r *http.Request) {
	if !h.validCSRF(r) {
		h.csrfError(w, r)
		return
	}
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		h.redirectToLogin(w, r)
		return
	}
	if err := h.auth.DeleteAllSessions(r.Context(), principal.User.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.clearSessionCookie(w)
	redirect(w, r, "/login")
}

func tokenExpiry(value string) (*time.Time, error) {
	var lifetime time.Duration
	switch value {
	case "", "90d":
		lifetime = defaultTokenLifetime
	case "30d":
		lifetime = 30 * 24 * time.Hour
	case "365d":
		lifetime = 365 * 24 * time.Hour
	case "never":
		return nil, nil
	default:
		return nil, errors.New("invalid token expiration")
	}
	expires := time.Now().Add(lifetime)
	return &expires, nil
}
