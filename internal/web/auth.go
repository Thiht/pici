package web

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Thiht/pici/internal/auth"
)

const oauthCookieMaxAge = 10 * 60

type loginPage struct {
	base
	Error      string
	Next       string
	Provider   string
	AccessNoun string
	AuthPath   string
	HasTeams   bool
	HasUsers   bool
}

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.alreadySignedIn(r) {
		redirect(w, r, "/")
		return
	}
	provider, accessNoun := "GitHub", "team"
	if h.auth.ProviderName() == "gitlab" {
		provider, accessNoun = "GitLab", "group"
	}
	data := loginPage{
		base:       h.base(w, r, "Sign in", ""),
		Next:       safeNext(r.URL.Query().Get("next")),
		Error:      loginError(r.URL.Query().Get("error"), provider),
		Provider:   provider,
		AccessNoun: accessNoun,
		AuthPath:   "/auth/" + h.auth.ProviderName(),
		HasTeams:   h.auth.HasTeams(),
		HasUsers:   h.auth.HasUsers(),
	}
	h.render(w, r, http.StatusOK, "login", data)
}

// LoginStart sets an OAuth state cookie and sends the user to GitHub.
func (h *Handler) LoginStart(w http.ResponseWriter, r *http.Request) {
	if h.alreadySignedIn(r) {
		redirect(w, r, "/")
		return
	}
	state, err := auth.NewState()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	verifier, err := auth.NewVerifier()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	next := safeNext(r.URL.Query().Get("next"))
	h.setOAuthCookies(w, state, verifier, next)
	http.Redirect(w, r, h.auth.LoginURL(state, verifier), http.StatusSeeOther)
}

// LoginCallback verifies the OAuth state, exchanges the code, and creates a
// session for an authorized team member.
func (h *Handler) LoginCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(auth.OAuthStateCookie)
	if err != nil || stateCookie.Value == "" ||
		subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(r.URL.Query().Get("state"))) != 1 {
		h.clearOAuthCookies(w)
		redirect(w, r, "/login?error=state")
		return
	}
	next := ""
	if cookie, err := r.Cookie(auth.OAuthNextCookie); err == nil {
		next = safeNext(cookie.Value)
	}
	verifier := ""
	if cookie, err := r.Cookie(auth.OAuthVerifierCookie); err == nil {
		verifier = cookie.Value
	}
	h.clearOAuthCookies(w)
	if verifier == "" {
		redirect(w, r, loginErrorURL(next, "state"))
		return
	}

	if r.URL.Query().Get("error") != "" {
		redirect(w, r, loginErrorURL(next, "denied"))
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		redirect(w, r, loginErrorURL(next, "state"))
		return
	}

	user, err := h.auth.Login(r.Context(), code, verifier)
	if errors.Is(err, auth.ErrAccessDenied) {
		redirect(w, r, loginErrorURL(next, "not_member"))
		return
	}
	if err != nil {
		slog.Error("provider login failed", "provider", h.auth.ProviderName(), "error", err)
		redirect(w, r, loginErrorURL(next, "provider"))
		return
	}

	raw, _, err := h.auth.CreateSession(r.Context(), user.ID, r.UserAgent())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.setSessionCookie(w, raw)
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
	if principal, ok := auth.FromContext(r.Context()); ok && principal.Session != nil {
		if err := h.auth.DeleteSessionForUser(r.Context(), principal.User.ID, principal.Session.ID); err != nil {
			slog.Warn("delete session", "error", err)
		}
	}
	h.clearSessionCookie(w)
	redirect(w, r, "/login")
}

// sessionAuth protects every UI route except the login flow and static assets.
func (h *Handler) sessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.isPublicWebPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(auth.SessionCookie)
		if err != nil {
			h.redirectToLogin(w, r)
			return
		}
		user, session, err := h.auth.AuthenticateSession(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthorized) {
				h.redirectToLogin(w, r)
				return
			}
			h.serverError(w, r, err)
			return
		}
		// Refresh the browser-side expiry too, so an active session keeps
		// sliding instead of dying 30 days after login.
		h.setSessionCookie(w, cookie.Value)
		principal := auth.Principal{User: user.WithoutOAuthTokens(), Session: &session}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func (h *Handler) isPublicWebPath(path string) bool {
	authPath := "/auth/" + h.auth.ProviderName()
	return path == "/login" ||
		path == authPath ||
		path == authPath+"/callback" ||
		strings.HasPrefix(path, "/static/")
}

func (h *Handler) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	target := "/login"
	if r.Method == http.MethodGet {
		target += "?next=" + url.QueryEscape(r.URL.RequestURI())
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *Handler) alreadySignedIn(r *http.Request) bool {
	cookie, err := r.Cookie(auth.SessionCookie)
	if err != nil {
		return false
	}
	_, _, err = h.auth.AuthenticateSession(r.Context(), cookie.Value)
	return err == nil
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    raw,
		Path:     "/",
		MaxAge:   int(h.auth.SessionTTL().Seconds()),
		HttpOnly: true,
		Secure:   h.auth.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.auth.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) setOAuthCookies(w http.ResponseWriter, state, verifier, next string) {
	for name, value := range map[string]string{
		auth.OAuthStateCookie:    state,
		auth.OAuthVerifierCookie: verifier,
		auth.OAuthNextCookie:     next,
	} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    value,
			Path:     "/",
			MaxAge:   oauthCookieMaxAge,
			HttpOnly: true,
			Secure:   h.auth.SecureCookies(),
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func (h *Handler) clearOAuthCookies(w http.ResponseWriter) {
	for _, name := range []string{auth.OAuthStateCookie, auth.OAuthVerifierCookie, auth.OAuthNextCookie} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   h.auth.SecureCookies(),
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func loginErrorURL(next, code string) string {
	values := url.Values{"error": {code}}
	if next != "" {
		values.Set("next", next)
	}
	return "/login?" + values.Encode()
}

func loginError(code, provider string) string {
	switch code {
	case "not_member":
		return "Your " + provider + " account is not authorized for this pici instance."
	case "denied":
		return provider + " sign-in was cancelled."
	case "state":
		return "Your sign-in session expired. Please try again."
	case "provider":
		return provider + " is unavailable right now. Please try again."
	default:
		return ""
	}
}

// safeNext only allows local, absolute paths so a crafted ?next= cannot
// redirect the user off-site after login.
func safeNext(next string) string {
	if next == "" || strings.ContainsRune(next, '\\') {
		return ""
	}
	u, err := url.Parse(next)
	if err != nil || u.IsAbs() || u.Host != "" || u.Scheme != "" {
		return ""
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return ""
	}
	return next
}
