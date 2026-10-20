package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGitLabProviderExchangeAndGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v4/") {
			if got := r.Header.Get("Authorization"); got != "Bearer glpat_test" {
				t.Errorf("unexpected authorization %q", got)
			}
		}
		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if got := r.FormValue("code_verifier"); got != "verifier" {
				t.Errorf("unexpected PKCE verifier %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"glpat_test","token_type":"bearer","refresh_token":"glrt_test","expires_in":7200,"scope":"read_api offline_access"}`))
		case "/api/v4/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"username":"alice","name":"Alice","avatar_url":"https://example.com/a.png"}`))
		case "/api/v4/groups":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`[{"full_path":"acme/sub/pici"}]`))
				return
			}
			if got := r.URL.Query().Get("min_access_level"); got != "10" {
				t.Errorf("unexpected min_access_level %q", got)
			}
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{"full_path":"acme"},{"full_path":"acme/pici"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewGitLabProvider(server.URL, "client-id", "client-secret", "https://pici.example.com/auth/gitlab/callback")

	identity, err := provider.Exchange(context.Background(), "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if identity.ProviderUserID != "42" || identity.Login != "alice" || identity.Name != "Alice" ||
		identity.AvatarURL != "https://example.com/a.png" || identity.Token.AccessToken != "glpat_test" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if identity.Token.RefreshToken != "glrt_test" || identity.Token.ExpiresAt == nil {
		t.Fatalf("expiring token not stored as such: %+v", identity.Token)
	}
	if !hasScope(identity.Scopes, "read_api") || !hasScope(identity.Scopes, "offline_access") {
		t.Fatalf("unexpected scopes: %#v", identity.Scopes)
	}

	teams, err := provider.Teams(context.Background(), "glpat_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 3 ||
		teams[0] != (Team{Path: "acme"}) ||
		teams[1] != (Team{Path: "acme/pici"}) ||
		teams[2] != (Team{Path: "acme/sub/pici"}) {
		t.Fatalf("unexpected groups: %#v", teams)
	}
}

func TestGitLabProviderErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"unauthorized", http.StatusUnauthorized, ErrTokenInvalid},
		{"not found", http.StatusNotFound, ErrTokenInvalid},
		{"forbidden", http.StatusForbidden, ErrAccessDenied},
		{"rate limited", http.StatusTooManyRequests, ErrProviderUnavailable},
		{"server error", http.StatusInternalServerError, ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			provider := NewGitLabProvider(server.URL, "id", "secret", "")
			_, err := provider.Teams(context.Background(), "token")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGitLabAuthCodeURL(t *testing.T) {
	provider := NewGitLabProvider("https://gitlab.example.com", "client-id", "secret", "https://pici.example.com/auth/gitlab/callback")
	raw := provider.AuthCodeURL("state-123", "nonce", "verifier")

	sum := sha256.Sum256([]byte("verifier"))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	for _, want := range []string{
		"https://gitlab.example.com/oauth/authorize?",
		"client_id=client-id",
		"redirect_uri=https%3A%2F%2Fpici.example.com%2Fauth%2Fgitlab%2Fcallback",
		"scope=read_api+offline_access",
		"state=state-123",
		"code_challenge=" + challenge,
		"code_challenge_method=S256",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("expected %q in %s", want, raw)
		}
	}
}

func TestGitLabProviderRefresh(t *testing.T) {
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"glpat_new","token_type":"bearer","refresh_token":"glrt_new","expires_in":7200,"scope":"read_api offline_access"}`))
	}))
	defer server.Close()

	provider := NewGitLabProvider(server.URL, "id", "secret", "")
	token, err := provider.Refresh(context.Background(), "glrt_old")
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "glrt_old" {
		t.Fatalf("unexpected refresh request: %v", form)
	}
	if token.AccessToken != "glpat_new" || token.RefreshToken != "glrt_new" || token.ExpiresAt == nil {
		t.Fatalf("unexpected refreshed token: %+v", token)
	}
}

func TestGitLabProviderRefreshErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"rejected refresh token", http.StatusOK, `{"error":"invalid_grant","error_description":"The provided authorization grant is invalid."}`, ErrTokenInvalid},
		{"provider error", http.StatusInternalServerError, `{"error":"server_error"}`, ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			provider := NewGitLabProvider(server.URL, "id", "secret", "")
			if _, err := provider.Refresh(context.Background(), "glrt_old"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGitLabVerify(t *testing.T) {
	cases := []struct {
		status  int
		wantErr error
	}{
		{http.StatusOK, nil},
		{http.StatusUnauthorized, ErrTokenInvalid},
		{http.StatusInternalServerError, ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v4/user" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write([]byte(`{"id":42,"username":"alice"}`))
				}
			}))
			defer server.Close()
			provider := NewGitLabProvider(server.URL, "id", "secret", "")
			err := provider.Verify(context.Background(), "token")
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGitLabProviderExpiringToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"glpat_test","token_type":"bearer","expires_in":7200,"scope":"read_api"}`))
		case "/api/v4/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"username":"alice"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewGitLabProvider(server.URL, "id", "secret", "")
	identity, err := provider.Exchange(context.Background(), "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Token.ExpiresAt == nil || time.Until(*identity.Token.ExpiresAt) < time.Hour {
		t.Fatalf("unexpected expiry: %v", identity.Token.ExpiresAt)
	}
}

func TestGitLabRequiredScope(t *testing.T) {
	provider := NewGitLabProvider("https://gitlab.com", "id", "secret", "")
	if got := provider.RequiredScope(); got != "" {
		t.Fatalf("RequiredScope = %q, want empty", got)
	}
}
