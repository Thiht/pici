package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/auth"
	"github.com/Thiht/pici/internal/auth/authtest"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func testTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func newTestStore(t *testing.T) stores.Store {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newTestAuthService(t *testing.T, store stores.Store) *auth.Service {
	t.Helper()
	return auth.NewService(store, &authtest.Provider{
		ExchangeErr: errors.New("unused"),
		RefreshErr:  errors.New("unused"),
	}, []string{"acme/pici"}, nil, 24*time.Hour, false)
}

func newTestUser(t *testing.T, store stores.Store) stores.User {
	t.Helper()
	now := time.Now()
	user, err := store.UpsertUser(context.Background(), stores.User{
		ID:               uuid.New(),
		Provider:         "github",
		ProviderUserID:   "1",
		Login:            "tester",
		OAuthAccessToken: "oauth",
		CreatedAt:        now,
		LastLoginAt:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func newTestAPIToken(t *testing.T, store stores.Store, userID uuid.UUID, token string) {
	t.Helper()
	if token == "" {
		token = "secret"
	}
	if err := store.CreateAPIToken(context.Background(), stores.APIToken{
		ID:        uuid.New(),
		UserID:    userID,
		Name:      "test",
		Prefix:    token[:min(len(token), 8)],
		TokenHash: testTokenHash(token),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func newAuthedServer(t *testing.T, token string) *Handler {
	t.Helper()
	store := newTestStore(t)
	svc := newTestAuthService(t, store)
	user := newTestUser(t, store)
	newTestAPIToken(t, store, user.ID, token)
	return newTestHandler(store, &ci.Runner{Store: store}, nil, t.TempDir(), 512<<20, svc)
}

func doReq(t *testing.T, s *Handler, method, path, header, value string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if header != "" {
		req.Header.Set(header, value)
	}
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	return rr
}

func doAuthedReq(t *testing.T, s *Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	return doReq(t, s, method, path, "Authorization", "Bearer secret")
}

func serveAuthed(t *testing.T, s *Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	return rr
}

func TestAuthMissingToken(t *testing.T) {
	s := newAuthedServer(t, "secret")
	rr := doReq(t, s, http.MethodGet, "/api/projects", "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthBearerToken(t *testing.T) {
	s := newAuthedServer(t, "secret")
	rr := doReq(t, s, http.MethodGet, "/api/projects", "Authorization", "Bearer secret")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAuthXAPIToken(t *testing.T) {
	s := newAuthedServer(t, "secret")
	rr := doReq(t, s, http.MethodGet, "/api/projects", "X-API-Token", "secret")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAuthWrongToken(t *testing.T) {
	s := newAuthedServer(t, "secret")
	rr := doReq(t, s, http.MethodGet, "/api/projects", "Authorization", "Bearer wrong")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthHealthOpen(t *testing.T) {
	s := newAuthedServer(t, "secret")
	rr := doReq(t, s, http.MethodGet, "/health", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for health, got %d", rr.Code)
	}
}

func TestAuthWebhookExempt(t *testing.T) {
	s := newAuthedServer(t, "secret")
	rr := doReq(t, s, http.MethodPost, "/api/webhooks/github/demo", "", "")
	if rr.Code == http.StatusUnauthorized {
		t.Fatalf("webhooks should not require the API token, got 401")
	}
}

func TestAuthDisabledUser(t *testing.T) {
	s := newAuthedServer(t, "secret")
	users, err := s.projects.store.ListActiveUsers(context.Background())
	if err != nil || len(users) != 1 {
		t.Fatalf("expected one active user, got %d (err=%v)", len(users), err)
	}
	if err := s.projects.store.DisableUser(context.Background(), users[0].ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	rr := doReq(t, s, http.MethodGet, "/api/projects", "Authorization", "Bearer secret")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for disabled user, got %d", rr.Code)
	}
}
