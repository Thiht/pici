package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Thiht/pici/internal/stores"
)

func TestBadgeStatus(t *testing.T) {
	dir, _ := makeRepo(t, map[string]string{})
	s, store := newWebhookServer(t, dir, "")

	exec := stores.Execution{
		ProjectID: testProjectID,
		Workflow:  "build",
		Ref:       "main",
		Status:    stores.StatusSuccess,
		Trigger:   stores.TriggerManual,
		Source:    stores.SourceGit,
		CreatedAt: time.Now(),
	}
	if err := store.CreateExecution(t.Context(), &exec); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/badge/demo?workflow=build", nil)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Fatalf("expected svg content type, got %q", ct)
	}
	out := rr.Body.String()
	for _, want := range []string{"<svg", "passing", ">build<"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in badge, got:\n%s", want, out)
		}
	}
}

func TestBadgeUnknownWorkflow(t *testing.T) {
	dir, _ := makeRepo(t, map[string]string{})
	s, _ := newWebhookServer(t, dir, "")

	req := httptest.NewRequest(http.MethodGet, "/badge/demo?workflow=missing", nil)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unknown") {
		t.Fatalf("expected unknown status, got:\n%s", rr.Body.String())
	}
}

func TestBadgeExemptFromAuth(t *testing.T) {
	store, err := stores.Open("sqlite", t.TempDir()+"/test.db", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.CreateProject(t.Context(), stores.Project{
		ID:            testProjectID,
		Name:          "demo",
		RepoURL:       "https://github.com/acme/demo.git",
		Provider:      stores.ProviderGithub,
		AuthType:      stores.AuthTypeNone,
		DefaultBranch: "main",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	s := New(store, nil, nil, t.TempDir(), "/workspace", 512<<20, "secret", "dev")

	req := httptest.NewRequest(http.MethodGet, "/badge/demo", nil)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized {
		t.Fatalf("badge should not require a token, got %d", rr.Code)
	}
}
