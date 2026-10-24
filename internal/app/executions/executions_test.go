package executions

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

type fixture struct {
	module  *Module
	ws      *workspace.Workspace
	project stores.Project
	first   stores.Execution
	second  stores.Execution
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	store, err := stores.Open("sqlite", filepath.Join(dir, "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	project := stores.Project{
		ID: uuid.New(), Name: "demo", RepoURL: "https://example.com/acme/demo.git",
		DefaultBranch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	project, err = store.CreateProject(ctx, project)
	if err != nil {
		t.Fatal(err)
	}

	first := stores.Execution{
		ProjectID: project.ID, Workflow: "build", Ref: "main", Status: stores.StatusFailed,
		CreatedAt: time.Now(),
		Steps: stores.Steps{
			{Name: "install", Status: stores.StepStatusSuccess},
			{Name: "test", Status: stores.StepStatusFailed},
		},
	}
	if err := store.CreateExecution(ctx, &first); err != nil {
		t.Fatal(err)
	}
	parentID, workspaceID := first.ID, first.ID
	second := stores.Execution{
		ProjectID: project.ID, ExecutionID: first.ID, Attempt: 2, Workflow: "build",
		Ref: "main", Status: stores.StatusSuccess, Trigger: stores.TriggerRetry,
		PreviousAttemptID: &parentID, WorkspaceID: &workspaceID, CreatedAt: time.Now(),
		Steps: stores.Steps{
			{Name: "install", Status: stores.StepStatusSuccess},
			{Name: "test", Status: stores.StepStatusSuccess},
		},
	}
	if err := store.CreateExecution(ctx, &second); err != nil {
		t.Fatal(err)
	}

	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	return fixture{
		module:  New(store, ws),
		ws:      ws,
		project: project,
		first:   first,
		second:  second,
	}
}

func TestGet(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	latest, err := f.module.Get(ctx, f.project.ID, f.first.ExecutionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Execution.ID != f.second.ID {
		t.Fatalf("attempt 0 should resolve to the latest attempt, got %d", latest.Execution.ID)
	}
	if len(latest.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(latest.Attempts))
	}
	if !latest.Latest {
		t.Fatal("the latest attempt should be marked as such")
	}

	first, err := f.module.Get(ctx, f.project.ID, f.first.ExecutionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Execution.ID != f.first.ID || first.Latest {
		t.Fatalf("unexpected first attempt: %+v", first)
	}
}

func TestGetNotFound(t *testing.T) {
	f := newFixture(t)

	if _, err := f.module.Get(context.Background(), f.project.ID, 9999, 0); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLogs(t *testing.T) {
	f := newFixture(t)
	writeLogs(t, f.ws, f.first)

	logs := f.module.Logs(f.first)
	defer logs.Close()
	data, err := io.ReadAll(logs)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "setup\ninstall\n"; got != want {
		t.Fatalf("Logs() = %q, want %q", got, want)
	}
}

func TestStepLog(t *testing.T) {
	f := newFixture(t)
	writeLogs(t, f.ws, f.first)

	logs, err := f.module.StepLog(f.first, "install")
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	data, err := io.ReadAll(logs)
	if err != nil || string(data) != "install\n" {
		t.Fatalf("StepLog(install) = %q, %v", data, err)
	}

	if _, err := f.module.StepLog(f.first, "missing"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown step, got %v", err)
	}
	if _, err := f.module.StepLog(f.first, "test"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a step without a log, got %v", err)
	}
}

func TestSetupLog(t *testing.T) {
	f := newFixture(t)
	writeLogs(t, f.ws, f.first)

	setup, err := f.module.SetupLog(f.first)
	if err != nil {
		t.Fatal(err)
	}
	defer setup.Close()
	data, err := io.ReadAll(setup)
	if err != nil || string(data) != "setup\n" {
		t.Fatalf("SetupLog() = %q, %v", data, err)
	}
}

func writeLogs(t *testing.T, ws *workspace.Workspace, exec stores.Execution) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(ws.StepLog(exec.ProjectID, exec.ID, 0, "install")), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		ws.SetupLog(exec.ProjectID, exec.ID):              "setup\n",
		ws.StepLog(exec.ProjectID, exec.ID, 0, "install"): "install\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
