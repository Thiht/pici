package handlers

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func seedProject(t *testing.T, s *Handler) stores.Project {
	t.Helper()
	now := time.Now()
	p, err := s.projects.store.CreateProject(context.Background(), stores.Project{
		ID:        uuid.New(),
		Name:      "demo",
		RepoURL:   "https://github.com/acme/demo.git",
		Provider:  stores.ProviderGeneric,
		AuthType:  stores.AuthTypeNone,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCacheGetUnavailableWithoutDocker(t *testing.T) {
	s := newAuthedServer(t, "")
	p := seedProject(t, s)
	rr := doReq(t, s, http.MethodGet, "/api/projects/"+p.ID.String()+"/cache", "", "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCacheGetUnknownProject(t *testing.T) {
	s := newAuthedServer(t, "")
	rr := doReq(t, s, http.MethodGet, "/api/projects/nope/cache", "", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestCacheDeleteRejectsForeignResource(t *testing.T) {
	s := newAuthedServer(t, "")
	p := seedProject(t, s)

	img := doReq(t, s, http.MethodDelete, "/api/projects/"+p.ID.String()+"/cache/images?reference=pici/other-build", "", "")
	if img.Code != http.StatusBadRequest {
		t.Fatalf("image: expected 400, got %d: %s", img.Code, img.Body.String())
	}
	vol := doReq(t, s, http.MethodDelete, "/api/projects/"+p.ID.String()+"/cache/volumes?name=pici-cache-other-node_modules", "", "")
	if vol.Code != http.StatusBadRequest {
		t.Fatalf("volume: expected 400, got %d: %s", vol.Code, vol.Body.String())
	}
}

func TestCacheDeleteUnavailableWithValidPrefix(t *testing.T) {
	s := newAuthedServer(t, "")
	p := seedProject(t, s)

	reference := ci.ImagePrefix(p.ID.String()) + "build"
	rr := doReq(t, s, http.MethodDelete, "/api/projects/"+p.ID.String()+"/cache/images?reference="+url.QueryEscape(reference), "", "")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rr.Code, rr.Body.String())
	}
}
