package projects

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
)

func newTestModule(t *testing.T) (*Module, stores.Store) {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store), store
}

func TestResolveByIdAndName(t *testing.T) {
	module, store := newTestModule(t)
	project := stores.Project{
		ID: uuid.New(), Name: "demo", RepoURL: "https://example.com/acme/demo.git",
		DefaultBranch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	created, err := store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{created.ID.String(), "demo"} {
		got, err := module.Resolve(context.Background(), ref)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", ref, err)
		}
		if got.ID != created.ID {
			t.Fatalf("Resolve(%q) = %v, want %v", ref, got.ID, created.ID)
		}
	}
}

func TestResolveNotFound(t *testing.T) {
	module, _ := newTestModule(t)

	for _, ref := range []string{"missing", uuid.New().String()} {
		if _, err := module.Resolve(context.Background(), ref); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("Resolve(%q) = %v, want ErrNotFound", ref, err)
		}
	}
}
