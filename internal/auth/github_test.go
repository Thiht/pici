package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGitHubProviderExchangeAndTeams(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/user") {
			if got := r.Header.Get("Authorization"); got != "Bearer gho_test" {
				t.Errorf("unexpected authorization %q", got)
			}
		}
		switch r.URL.Path {
		case "/login/oauth/access_token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if got := r.FormValue("code_verifier"); got != "verifier" {
				t.Errorf("unexpected PKCE verifier %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"gho_test","token_type":"bearer","scope":"read:org,repo"}`))
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"login":"alice","name":"Alice","avatar_url":"https://example.com/a.png"}`))
		case "/user/teams":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`[{"slug":"ops","organization":{"login":"acme"}}]`))
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/user/teams?per_page=100&page=2>; rel="next"`, server.URL))
			_, _ = w.Write([]byte(`[{"slug":"pici","organization":{"login":"acme"}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := newGitHubProvider(
		"client-id",
		"client-secret",
		"https://pici.example.com/auth/github/callback",
		server.URL+"/login/oauth/authorize",
		server.URL+"/login/oauth/access_token",
		server.URL,
		true,
	)

	identity, err := provider.Exchange(context.Background(), "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if identity.ProviderUserID != "42" || identity.Login != "alice" || identity.Name != "Alice" ||
		identity.AvatarURL != "https://example.com/a.png" || identity.Token.AccessToken != "gho_test" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if identity.Token.RefreshToken != "" || identity.Token.ExpiresAt != nil {
		t.Fatalf("non-expiring token stored as expiring: %+v", identity.Token)
	}
	if !hasScope(identity.Scopes, "read:org") || !hasScope(identity.Scopes, "repo") {
		t.Fatalf("unexpected scopes: %#v", identity.Scopes)
	}

	teams, err := provider.Teams(context.Background(), "gho_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 ||
		teams[0] != (Team{Path: "acme/pici"}) ||
		teams[1] != (Team{Path: "acme/ops"}) {
		t.Fatalf("unexpected teams: %#v", teams)
	}
}

func TestGitHubProviderErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		wantErr error
	}{
		{"unauthorized", http.StatusUnauthorized, nil, ErrTokenInvalid},
		{"not found", http.StatusNotFound, nil, ErrTokenInvalid},
		{"forbidden", http.StatusForbidden, nil, ErrTokenInvalid},
		{"rate limited", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, ErrProviderUnavailable},
		{"server error", http.StatusInternalServerError, nil, ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			provider := newGitHubProvider("id", "secret", "", server.URL+"/auth", server.URL+"/token", server.URL, true)
			_, err := provider.Teams(context.Background(), "token")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGitHubAuthCodeURL(t *testing.T) {
	provider := newGitHubProvider("client-id", "secret", "https://pici.example.com/auth/github/callback", githubAuthURL, githubTokenURL, githubAPIURL, true)
	raw := provider.AuthCodeURL("state-123", "nonce", "verifier")

	sum := sha256.Sum256([]byte("verifier"))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	for _, want := range []string{
		"client_id=client-id",
		"redirect_uri=https%3A%2F%2Fpici.example.com%2Fauth%2Fgithub%2Fcallback",
		"scope=read%3Aorg",
		"state=state-123",
		"code_challenge=" + challenge,
		"code_challenge_method=S256",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("expected %q in %s", want, raw)
		}
	}
}

func TestGitHubAuthCodeURLWithoutTeams(t *testing.T) {
	provider := newGitHubProvider("client-id", "secret", "https://pici.example.com/auth/github/callback", githubAuthURL, githubTokenURL, githubAPIURL, false)
	raw := provider.AuthCodeURL("state-123", "nonce", "verifier")
	if strings.Contains(raw, "scope=") {
		t.Fatalf("users-only provider should not request a scope: %s", raw)
	}
	if !strings.Contains(raw, "code_challenge_method=S256") {
		t.Fatalf("expected PKCE in the authorization URL: %s", raw)
	}
}

func TestGitHubProviderAcceptsExpiringTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"gho_test","token_type":"bearer","expires_in":28800,"refresh_token":"ghr_test","scope":"read:org"}`))
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"login":"alice","name":"Alice"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := newGitHubProvider("id", "secret", "", server.URL+"/auth", server.URL+"/login/oauth/access_token", server.URL, true)
	identity, err := provider.Exchange(context.Background(), "code", "nonce", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Token.AccessToken != "gho_test" || identity.Token.RefreshToken != "ghr_test" {
		t.Fatalf("unexpected token: %+v", identity.Token)
	}
	if identity.Token.ExpiresAt == nil || time.Until(*identity.Token.ExpiresAt) < 7*time.Hour {
		t.Fatalf("unexpected expiry: %v", identity.Token.ExpiresAt)
	}
}

func TestGitHubProviderRefresh(t *testing.T) {
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/access_token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"gho_new","token_type":"bearer","expires_in":28800,"refresh_token":"ghr_new","scope":"read:org"}`))
	}))
	defer server.Close()

	provider := newGitHubProvider("id", "secret", "", server.URL+"/auth", server.URL+"/login/oauth/access_token", server.URL, true)
	token, err := provider.Refresh(context.Background(), "ghr_old")
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "ghr_old" {
		t.Fatalf("unexpected refresh request: %v", form)
	}
	if token.AccessToken != "gho_new" || token.RefreshToken != "ghr_new" || token.ExpiresAt == nil {
		t.Fatalf("unexpected refreshed token: %+v", token)
	}
}

func TestGitHubProviderRefreshErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"rejected refresh token", http.StatusOK, `{"error":"invalid_grant","error_description":"The refresh token is invalid."}`, ErrTokenInvalid},
		{"misconfigured app", http.StatusOK, `{"error":"unauthorized_client"}`, ErrProviderUnavailable},
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

			provider := newGitHubProvider("id", "secret", "", server.URL+"/auth", server.URL+"/login/oauth/access_token", server.URL, true)
			if _, err := provider.Refresh(context.Background(), "ghr_old"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGitHubVerify(t *testing.T) {
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
				if r.URL.Path != "/user" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write([]byte(`{"id":42,"login":"alice"}`))
				}
			}))
			defer server.Close()
			provider := newGitHubProvider("id", "secret", "", server.URL+"/auth", server.URL+"/token", server.URL, false)
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

func TestParseScopes(t *testing.T) {
	got := parseScopes("read:org, repo,  user:email")
	if len(got) != 3 || got[0] != "read:org" || got[1] != "repo" || got[2] != "user:email" {
		t.Fatalf("parseScopes = %#v", got)
	}
}

func TestNextLink(t *testing.T) {
	header := `<https://api.github.com/user/teams?page=2>; rel="next", <https://api.github.com/user/teams?page=5>; rel="last"`
	if got := nextLink(header); got != "https://api.github.com/user/teams?page=2" {
		t.Fatalf("nextLink = %q", got)
	}
	if got := nextLink(""); got != "" {
		t.Fatalf("nextLink empty = %q", got)
	}
}
