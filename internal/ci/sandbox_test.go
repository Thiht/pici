package ci

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

func TestRunDockerWorkflowUsesSandbox(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := stores.Open("sqlite", filepath.Join(dir, "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	project, err := store.CreateProject(ctx, stores.Project{
		ID: uuid.New(), Name: "demo", RepoURL: "https://example.com/acme/demo.git",
		DefaultBranch: "main", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshotID := uuid.New()
	execution := stores.Execution{
		ProjectID: project.ID, Project: project.Name, Workflow: "build", Ref: "main",
		Status: stores.StatusPending, Source: stores.SourceSnapshot, SnapshotID: &snapshotID, CreatedAt: now,
	}
	if err := store.CreateExecution(ctx, &execution); err != nil {
		t.Fatal(err)
	}

	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	writeSnapshot(t, ws.Snapshot(project.ID, snapshotID), map[string]string{
		".ci/build/ci.yml": "steps:\n  - name: image\n    run: docker build -t app .\n    docker: true\n",
	})

	engine := &fakeEngine{}
	runner := &Runner{
		Store:              store,
		Engine:             engine,
		Workspace:          ws,
		MountPath:          "/workspace",
		PublicBaseURL:      "https://pici.test",
		DefaultStepTimeout: time.Minute,
		MaxSnapshotSize:    1 << 20,
		Registries:         docker.Registries{Images: "172.17.0.1:5000", HubCache: "172.17.0.1:5001"},
	}

	runner.run(ctx, execution)

	got, err := store.GetAttempt(ctx, project.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != stores.StatusSuccess {
		t.Fatalf("status = %q (error %q)", got.Status, got.Error)
	}
	if engine.sandboxes != 1 || engine.removed != 1 {
		t.Fatalf("expected one sandbox created and removed, got %d/%d", engine.sandboxes, engine.removed)
	}
	if engine.pushes != 1 || engine.pulls != 1 {
		t.Fatalf("expected the workflow image pushed and pulled, got %d/%d", engine.pushes, engine.pulls)
	}
	if !strings.HasPrefix(engine.lastRun.Image, "172.17.0.1:5000/pici/") {
		t.Fatalf("step image = %q, want the registry ref", engine.lastRun.Image)
	}
	if !slices.Contains(engine.lastRun.Env, "DOCKER_HOST=tcp://10.201.0.1:2375") {
		t.Fatalf("expected the sandbox DOCKER_HOST, got %v", engine.lastRun.Env)
	}
	if !slices.Equal(engine.lastRun.Binds, []string{"/workspace:/workspace"}) {
		t.Fatalf("expected step mounts to point inside the sandbox, got %v", engine.lastRun.Binds)
	}
}

func TestRunDockerWorkflowFailsWithoutRegistry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := stores.Open("sqlite", filepath.Join(dir, "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	project, err := store.CreateProject(ctx, stores.Project{
		ID: uuid.New(), Name: "demo", RepoURL: "https://example.com/acme/demo.git",
		DefaultBranch: "main", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshotID := uuid.New()
	execution := stores.Execution{
		ProjectID: project.ID, Project: project.Name, Workflow: "build", Ref: "main",
		Status: stores.StatusPending, Source: stores.SourceSnapshot, SnapshotID: &snapshotID, CreatedAt: now,
	}
	if err := store.CreateExecution(ctx, &execution); err != nil {
		t.Fatal(err)
	}

	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	writeSnapshot(t, ws.Snapshot(project.ID, snapshotID), map[string]string{
		".ci/build/ci.yml": "steps:\n  - name: image\n    run: docker build -t app .\n    docker: true\n",
	})

	engine := &fakeEngine{}
	runner := &Runner{
		Store: store, Engine: engine, Workspace: ws, MountPath: "/workspace",
		PublicBaseURL: "https://pici.test", DefaultStepTimeout: time.Minute, MaxSnapshotSize: 1 << 20,
	}
	runner.run(ctx, execution)

	got, err := store.GetAttempt(ctx, project.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != stores.StatusFailed || !strings.Contains(got.Error, "registry") {
		t.Fatalf("expected a missing-registry failure, got %q (%q)", got.Status, got.Error)
	}
	if engine.runs != 0 {
		t.Fatalf("no step may run without a sandbox, got %d runs", engine.runs)
	}
}
