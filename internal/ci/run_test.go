package ci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/docker"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

// fakeEngine stands in for Docker so run can be exercised without containers.
type fakeEngine struct {
	builds int
	runs   int
	host   string
}

func (f *fakeEngine) Build(context.Context, docker.BuildOptions) error {
	f.builds++
	return nil
}

func (f *fakeEngine) Run(_ context.Context, opts docker.RunOptions) (int, error) {
	f.runs++
	fmt.Fprint(opts.Logs, "hello from the fake engine\n")
	return 0, nil
}

func (f *fakeEngine) Host() string {
	if f.host == "" {
		return "unix:///var/run/docker.sock"
	}
	return f.host
}

func TestRunSnapshotExecution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := stores.Open("sqlite", filepath.Join(dir, "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	project := stores.Project{
		ID: uuid.New(), Name: "demo", RepoURL: "https://example.com/acme/demo.git",
		DefaultBranch: "main", CreatedAt: now, UpdatedAt: now,
	}
	project, err = store.CreateProject(ctx, project)
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
		".ci/build/ci.yml": "steps:\n  - name: test\n    run: echo hi\n",
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
	}

	runner.run(ctx, execution)

	got, err := store.GetAttempt(ctx, project.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != stores.StatusSuccess {
		t.Fatalf("status = %q (error %q)", got.Status, got.Error)
	}
	if len(got.Steps) != 1 || got.Steps[0].Name != "test" || got.Steps[0].Status != stores.StepStatusSuccess {
		t.Fatalf("unexpected steps: %+v", got.Steps)
	}
	if engine.builds != 1 || engine.runs != 1 {
		t.Fatalf("expected one build and one run, got %d/%d", engine.builds, engine.runs)
	}

	stepLog, err := os.ReadFile(ws.StepLog(project.ID, execution.ID, 0, "test"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stepLog), "hello from the fake engine") {
		t.Fatalf("step log = %q", stepLog)
	}
	setupLog, err := os.ReadFile(ws.SetupLog(project.ID, execution.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(setupLog), `pici: starting workflow "build" on main`) {
		t.Fatalf("setup log = %q", setupLog)
	}

	// The archive is no longer needed once the Execution is terminal.
	if _, err := os.Stat(ws.Snapshot(project.ID, snapshotID)); !os.IsNotExist(err) {
		t.Fatalf("snapshot should have been removed, got %v", err)
	}
}

func TestRunWithoutEngineFails(t *testing.T) {
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
		ProjectID: project.ID, Workflow: "build", Ref: "main",
		Status: stores.StatusPending, Source: stores.SourceSnapshot, SnapshotID: &snapshotID, CreatedAt: now,
	}
	if err := store.CreateExecution(ctx, &execution); err != nil {
		t.Fatal(err)
	}

	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	writeSnapshot(t, ws.Snapshot(project.ID, snapshotID), map[string]string{
		".ci/build/ci.yml": "steps:\n  - name: test\n    run: echo hi\n",
	})

	runner := &Runner{Store: store, Workspace: ws, MountPath: "/workspace", MaxSnapshotSize: 1 << 20}
	runner.run(ctx, execution)

	got, err := store.GetAttempt(ctx, project.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != stores.StatusFailed || !strings.Contains(got.Error, "container engine") {
		t.Fatalf("expected a missing-engine failure, got %q (%q)", got.Status, got.Error)
	}
}
