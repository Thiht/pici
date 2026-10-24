package executions

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

type fixture struct {
	module  *Module
	store   stores.Store
	ws      *workspace.Workspace
	runner  *fakeRunner
	project stores.Project
	first   stores.Execution
	second  stores.Execution
	snap    stores.Execution
}

// fakeRunner records what the module asks of the runner. The zero value
// queues nothing, so callers that only read never need it.
type fakeRunner struct {
	enqueue        []any
	snapshotID     uuid.UUID
	retryErr       error
	cancelled      []int64
	rebuilt        bool
	rebuiltFor     stores.Execution
	enqueuedReturn stores.Execution
}

func (f *fakeRunner) Enqueue(_ context.Context, _ stores.Project, workflow, ref, commitSHA string, trigger stores.Trigger, createdBy *uuid.UUID) (stores.Execution, error) {
	f.enqueue = append(f.enqueue, workflow, ref, trigger, createdBy)
	if f.enqueuedReturn.ID == 0 {
		return stores.Execution{ID: 9, ExecutionID: 9, Attempt: 1, AttemptsCount: 1}, nil
	}
	return f.enqueuedReturn, nil
}

func (f *fakeRunner) EnqueueSnapshot(_ context.Context, _ stores.Project, _, _, _ string, snapshotID uuid.UUID, _ *uuid.UUID) (stores.Execution, error) {
	f.snapshotID = snapshotID
	return stores.Execution{ID: 10, ExecutionID: 10, Attempt: 1, AttemptsCount: 1}, nil
}

func (f *fakeRunner) EnqueueRebuild(_ context.Context, _ stores.Project, previous stores.Execution, _ *uuid.UUID) (stores.Execution, error) {
	f.rebuilt = true
	f.rebuiltFor = previous
	return stores.Execution{ID: 11, ExecutionID: 11, Attempt: 1, AttemptsCount: 1}, nil
}

func (f *fakeRunner) EnqueueRetry(_ context.Context, _ stores.Project, _ stores.Execution, _ *uuid.UUID) (stores.Execution, error) {
	if f.retryErr != nil {
		return stores.Execution{}, f.retryErr
	}
	return stores.Execution{ID: 12, ExecutionID: 3, Attempt: 3, AttemptsCount: 3}, nil
}

func (f *fakeRunner) Cancel(_ uuid.UUID, execID int64) {
	f.cancelled = append(f.cancelled, execID)
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
	snap := stores.Execution{
		ProjectID: project.ID, Workflow: "build", Ref: "main", Status: stores.StatusFailed,
		Source: stores.SourceSnapshot, SnapshotID: new(uuid.New()), CreatedAt: time.Now(),
	}
	if err := store.CreateExecution(ctx, &snap); err != nil {
		t.Fatal(err)
	}

	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	runner := &fakeRunner{}
	return fixture{
		module:  New(store, ws, ws, runner),
		store:   store,
		ws:      ws,
		runner:  runner,
		project: project,
		first:   first,
		second:  second,
		snap:    snap,
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
	if len(latest.Attempts) != 2 || !latest.Latest {
		t.Fatalf("unexpected view: %+v", latest)
	}

	first, err := f.module.Get(ctx, f.project.ID, f.first.ExecutionID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Execution.ID != f.first.ID || first.Latest {
		t.Fatalf("unexpected first attempt: %+v", first)
	}

	if _, err := f.module.Get(ctx, f.project.ID, 9999, 0); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	execution, err := f.module.Create(ctx, f.project, "build", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if execution.ID == 0 {
		t.Fatalf("expected the queued execution, got %+v", execution)
	}
	want := []any{"build", "main", stores.TriggerManual, (*uuid.UUID)(nil)}
	if len(f.runner.enqueue) != len(want) {
		t.Fatalf("unexpected enqueue call: %v", f.runner.enqueue)
	}
	for i := range want {
		if f.runner.enqueue[i] != want[i] {
			t.Fatalf("enqueue[%d] = %v, want %v", i, f.runner.enqueue[i], want[i])
		}
	}

	if _, err := f.module.Create(ctx, f.project, "  ", "main", nil); !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for an empty workflow, got %v", err)
	}
}

func TestCreateSnapshotStoresArchive(t *testing.T) {
	f := newFixture(t)
	archive := "archive-bytes"

	if _, err := f.module.CreateSnapshot(context.Background(), f.project, "build", "", "", strings.NewReader(archive), nil); err != nil {
		t.Fatal(err)
	}
	path := f.ws.Snapshot(f.project.ID, f.runner.snapshotID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the archive at %s: %v", path, err)
	}
	if string(data) != archive {
		t.Fatalf("archive content = %q", data)
	}
}

func TestCancel(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.module.Cancel(ctx, f.project.ID, f.first.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if len(f.runner.cancelled) != 1 || f.runner.cancelled[0] != f.second.ID {
		t.Fatalf("expected the latest attempt to be cancelled, got %v", f.runner.cancelled)
	}
	canceled, err := f.store.IsCancelRequested(ctx, f.project.ID, f.second.ID)
	if err != nil || !canceled {
		t.Fatalf("cancel was not requested: %v %v", canceled, err)
	}
}

func TestRebuild(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if _, err := f.module.Rebuild(ctx, f.project, f.first.ExecutionID, nil); err != nil {
		t.Fatal(err)
	}
	if !f.runner.rebuilt || f.runner.rebuiltFor.ID != f.second.ID {
		t.Fatalf("expected a rebuild of the latest attempt, got %+v", f.runner.rebuiltFor)
	}

	if _, err := f.module.Rebuild(ctx, f.project, f.snap.ExecutionID, nil); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("expected ErrConflict for a snapshot execution, got %v", err)
	}
}

func TestRetryMapsRunnerErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.runner.retryErr = ci.ErrNotRetryable
	if _, err := f.module.Retry(ctx, f.project, f.first.ExecutionID, 0, nil); !errors.Is(err, app.ErrNotRetryable) {
		t.Fatalf("expected ErrNotRetryable, got %v", err)
	}

	f.runner.retryErr = ci.ErrWorkspaceGone
	if _, err := f.module.Retry(ctx, f.project, f.first.ExecutionID, 0, nil); !errors.Is(err, app.ErrWorkspaceGone) {
		t.Fatalf("expected ErrWorkspaceGone, got %v", err)
	}
}

func TestRetryReturnsLatestAttempt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	execution, err := f.module.Retry(ctx, f.project, f.first.ExecutionID, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if execution.ExecutionID != f.first.ExecutionID || execution.Attempt != 2 {
		t.Fatalf("expected the execution with its latest attempt, got %+v", execution)
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
