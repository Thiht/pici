package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func newAuthedServer(t *testing.T, token string) *Handler {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store, &ci.Runner{Store: store}, nil, t.TempDir(), "/workspace", token, "dev")
}

func doReq(t *testing.T, s *Handler, method, path string, header, value string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if header != "" {
		req.Header.Set(header, value)
	}
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

func TestAuthDisabled(t *testing.T) {
	s := newAuthedServer(t, "")
	rr := doReq(t, s, http.MethodGet, "/api/projects", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with no token configured, got %d", rr.Code)
	}
}
