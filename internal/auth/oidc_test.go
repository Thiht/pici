package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

type oidcTestServer struct {
	server         *httptest.Server
	signer         jose.Signer
	nonce          string
	userInfoStatus int
	refreshCalls   int
	lastRefresh    url.Values
}

func newOIDCTestServer(t *testing.T, nonce string) *oidcTestServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	if err != nil {
		t.Fatal(err)
	}

	s := &oidcTestServer{signer: signer, nonce: nonce}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                s.server.URL,
			"authorization_endpoint":                s.server.URL + "/oauth/authorize",
			"token_endpoint":                        s.server.URL + "/oauth/token",
			"userinfo_endpoint":                     s.server.URL + "/oauth/userinfo",
			"jwks_uri":                              s.server.URL + "/oauth/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/oauth/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		e := big.NewInt(int64(key.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "test-key",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(e),
			}},
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}
		if r.FormValue("grant_type") == "refresh_token" {
			s.refreshCalls++
			s.lastRefresh = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "refreshed-access-token",
				"token_type":    "bearer",
				"refresh_token": "refreshed-refresh-token",
				"expires_in":    3600,
				"scope":         "openid profile email offline_access",
			})
			return
		}
		if got := r.FormValue("code_verifier"); got != "verifier" {
			t.Errorf("unexpected PKCE verifier %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-token",
			"token_type":    "bearer",
			"refresh_token": "refresh-token",
			"expires_in":    3600,
			"scope":         "openid profile email offline_access",
			"id_token":      s.idToken(t),
		})
	})
	mux.HandleFunc("/oauth/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-token" {
			t.Errorf("unexpected authorization %q", r.Header.Get("Authorization"))
		}
		if s.userInfoStatus != 0 && s.userInfoStatus != http.StatusOK {
			w.WriteHeader(s.userInfoStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":                "subject-1",
			"preferred_username": "alice",
			"name":               "Alice",
			"email":              "alice@example.com",
			"email_verified":     true,
			"picture":            "https://example.com/a.png",
			"groups":             []string{"acme/pici", "acme/sub/pici"},
		})
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func (s *oidcTestServer) idToken(t *testing.T) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"iss":                s.server.URL,
		"aud":                "client-id",
		"sub":                "subject-1",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"nonce":              s.nonce,
		"preferred_username": "alice",
		"name":               "Alice",
		"email":              "alice@example.com",
		"picture":            "https://example.com/a.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	jws, err := s.signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newTestOIDCProvider(t *testing.T, server *oidcTestServer) *OIDCProvider {
	t.Helper()
	provider, err := NewOIDCProvider(
		context.Background(),
		server.server.URL,
		"client-id",
		"client-secret",
		"https://pici.example.com/auth/oidc/callback",
		"Keycloak",
		[]string{"openid", "profile", "email", "offline_access"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestOIDCProviderExchangeAndGroups(t *testing.T) {
	server := newOIDCTestServer(t, "nonce-123")
	provider := newTestOIDCProvider(t, server)
	ctx := context.Background()

	identity, err := provider.Exchange(ctx, "code", "nonce-123", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if identity.ProviderUserID != "subject-1" || identity.Login != "alice" || identity.Name != "Alice" ||
		identity.AvatarURL != "https://example.com/a.png" || identity.Token.AccessToken != "access-token" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if identity.Token.RefreshToken != "refresh-token" || identity.Token.ExpiresAt == nil {
		t.Fatalf("expiring token not stored as such: %+v", identity.Token)
	}
	if !hasScope(identity.Scopes, "openid") || !hasScope(identity.Scopes, "offline_access") {
		t.Fatalf("unexpected scopes: %#v", identity.Scopes)
	}

	teams, err := provider.Teams(ctx, "access-token")
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 ||
		teams[0] != (Team{Path: "acme/pici"}) ||
		teams[1] != (Team{Path: "acme/sub/pici"}) {
		t.Fatalf("unexpected groups: %#v", teams)
	}
	if err := provider.Verify(ctx, "access-token"); err != nil {
		t.Fatal(err)
	}
	if provider.DisplayName() != "Keycloak" || provider.AccessNoun() != "group" {
		t.Fatalf("unexpected provider metadata: %q %q", provider.DisplayName(), provider.AccessNoun())
	}
}

func TestOIDCProviderRejectsWrongNonce(t *testing.T) {
	server := newOIDCTestServer(t, "nonce-123")
	provider := newTestOIDCProvider(t, server)

	if _, err := provider.Exchange(context.Background(), "code", "other-nonce", "verifier"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestOIDCAuthCodeURL(t *testing.T) {
	server := newOIDCTestServer(t, "nonce-123")
	provider := newTestOIDCProvider(t, server)
	raw := provider.AuthCodeURL("state-123", "nonce-123", "verifier")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	for key, want := range map[string]string{
		"client_id":             "client-id",
		"redirect_uri":          "https://pici.example.com/auth/oidc/callback",
		"scope":                 "openid profile email offline_access",
		"state":                 "state-123",
		"nonce":                 "nonce-123",
		"access_type":           "offline",
		"code_challenge_method": "S256",
	} {
		if got := query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if query.Get("code_challenge") == "" {
		t.Error("expected a PKCE code challenge")
	}
}

func TestOIDCProviderRefresh(t *testing.T) {
	server := newOIDCTestServer(t, "nonce-123")
	provider := newTestOIDCProvider(t, server)

	token, err := provider.Refresh(context.Background(), "refresh-token")
	if err != nil {
		t.Fatal(err)
	}
	if server.refreshCalls != 1 || server.lastRefresh.Get("refresh_token") != "refresh-token" {
		t.Fatalf("unexpected refresh request: %d %v", server.refreshCalls, server.lastRefresh)
	}
	if token.AccessToken != "refreshed-access-token" || token.RefreshToken != "refreshed-refresh-token" || token.ExpiresAt == nil {
		t.Fatalf("unexpected refreshed token: %+v", token)
	}
}

func TestOIDCProviderUserInfoErrors(t *testing.T) {
	cases := []struct {
		status  int
		wantErr error
	}{
		{http.StatusUnauthorized, ErrTokenInvalid},
		{http.StatusForbidden, ErrProviderUnavailable},
		{http.StatusTooManyRequests, ErrProviderUnavailable},
		{http.StatusInternalServerError, ErrProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := newOIDCTestServer(t, "nonce-123")
			server.userInfoStatus = tc.status
			provider := newTestOIDCProvider(t, server)
			_, err := provider.Teams(context.Background(), "access-token")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestOIDCDiscoveryFailure(t *testing.T) {
	if _, err := NewOIDCProvider(context.Background(), "http://127.0.0.1:1", "id", "secret", "", "SSO", []string{"openid"}); err == nil {
		t.Fatal("expected an error when discovery fails")
	}
}
