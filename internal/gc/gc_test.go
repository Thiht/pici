package gc

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
)

func newTestStore(t *testing.T) stores.Store {
	t.Helper()
	s, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCleanupUploads(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "uploads", "11111111-1111-1111-1111-111111111111")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old.tar.gz")
	stale := filepath.Join(dir, "stale.tar.gz")
	fresh := filepath.Join(dir, "fresh.tar.gz")
	for _, p := range []string{old, stale, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}

	c := &Collector{WorkspaceDir: ws, LogsDir: filepath.Join(ws, "logs"), Keep: time.Hour}
	c.cleanupUploads()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expected old upload to be removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("expected fresh upload to be kept")
	}
}

func TestCleanupWorkspacesRetainsReferenced(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	ws := t.TempDir()
	logs := t.TempDir()
	projectID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	if _, err := store.CreateProject(ctx, stores.Project{ID: projectID, Name: "demo", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-2 * time.Hour)
	owner := stores.Execution{ProjectID: projectID, Workflow: "build", Status: stores.StatusFailed, FinishedAt: &past, CreatedAt: past}
	if err := store.CreateExecution(ctx, &owner); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(ws, projectID.String(), strconv.FormatInt(owner.ID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	c := &Collector{Store: store, WorkspaceDir: ws, LogsDir: logs, Keep: time.Hour}

	parentID, workspaceID := owner.ID, owner.ID
	child := stores.Execution{ProjectID: projectID, Workflow: "build", Status: stores.StatusPending, ParentID: &parentID, WorkspaceID: &workspaceID, CreatedAt: time.Now()}
	if err := store.CreateExecution(ctx, &child); err != nil {
		t.Fatal(err)
	}
	c.cleanupWorkspaces(ctx)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("workspace should be retained while a child is pending: %v", err)
	}

	child.Status = stores.StatusSuccess
	child.FinishedAt = &past
	if err := store.UpdateExecution(ctx, child); err != nil {
		t.Fatal(err)
	}
	c.cleanupWorkspaces(ctx)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("workspace should be removed once all references are expired")
	}
}

type fakeEngine struct {
	sandboxes []docker.SandboxRef
	removed   []string
}

func (f *fakeEngine) PruneImages(context.Context) error { return nil }

func (f *fakeEngine) Sandboxes(context.Context) ([]docker.SandboxRef, error) { return f.sandboxes, nil }

func (f *fakeEngine) RemoveSandbox(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return nil
}

type fakeRegistry struct {
	usage  int64
	purged bool
}

func (f *fakeRegistry) RegistrySize(context.Context) (int64, error) { return f.usage, nil }

func (f *fakeRegistry) PurgeRegistry(context.Context) error {
	f.purged = true
	return nil
}

func TestCleanupSandboxesReapsNonRunningAttempts(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	projectID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	if _, err := store.CreateProject(ctx, stores.Project{ID: projectID, Name: "demo", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	statuses := []stores.Status{stores.StatusRunning, stores.StatusSuccess, stores.StatusPending, stores.StatusCanceled}
	sandboxes := make([]docker.SandboxRef, 0, len(statuses)+1)
	for _, status := range statuses {
		exec := stores.Execution{ProjectID: projectID, Workflow: "build", Status: status, CreatedAt: time.Now()}
		if err := store.CreateExecution(ctx, &exec); err != nil {
			t.Fatal(err)
		}
		sandboxes = append(sandboxes, docker.SandboxRef{ID: "sandbox-" + string(status), ProjectID: projectID.String(), Attempt: exec.ID})
	}
	sandboxes = append(sandboxes, docker.SandboxRef{ID: "sandbox-gone", ProjectID: projectID.String(), Attempt: 9999})

	engine := &fakeEngine{sandboxes: sandboxes}
	c := &Collector{Store: store, Engine: engine}
	c.cleanupSandboxes(ctx)

	if len(engine.removed) != 4 {
		t.Fatalf("expected four sandboxes removed, got %v", engine.removed)
	}
	for _, id := range engine.removed {
		if id == "sandbox-running" {
			t.Fatal("a running attempt's sandbox must be kept")
		}
	}
}

func TestCleanupRegistryCachePurgesOverCap(t *testing.T) {
	registry := &fakeRegistry{usage: 2048}
	c := &Collector{Registry: registry, RegistryCacheMaxSize: 1024}
	c.cleanupRegistryCache(context.Background())
	if !registry.purged {
		t.Fatal("expected the image cache to be purged over its cap")
	}

	registry = &fakeRegistry{usage: 512}
	c = &Collector{Registry: registry, RegistryCacheMaxSize: 1024}
	c.cleanupRegistryCache(context.Background())
	if registry.purged {
		t.Fatal("expected the image cache to be kept under its cap")
	}
}
