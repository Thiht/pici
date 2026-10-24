package ci

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

func TestStreamLogs(t *testing.T) {
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
	execution := stores.Execution{
		ProjectID: project.ID, Workflow: "build", Ref: "main", Status: stores.StatusSuccess,
		CreatedAt: now, FinishedAt: &now,
		Steps: stores.Steps{{Name: "compile", Status: stores.StepStatusSuccess}},
	}
	if err := store.CreateExecution(ctx, &execution); err != nil {
		t.Fatal(err)
	}

	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	for path, content := range map[string]string{
		ws.SetupLog(project.ID, execution.ID):              "cloning\n",
		ws.StepLog(project.ID, execution.ID, 0, "compile"): "building\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runner := &Runner{Store: store, Workspace: ws}
	var events []StreamEvent
	err = runner.StreamLogs(ctx, project.ID, execution.ID, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	assertLine := func(event StreamEvent, source, line string) {
		t.Helper()
		log, ok := event.(LogEvent)
		if !ok || log.Source != source || string(log.Line) != line {
			t.Fatalf("expected log %q from %q, got %#v", line, source, event)
		}
	}
	assertLine(events[0], "setup", "cloning")
	assertLine(events[1], "compile", "building")

	state, ok := events[2].(StateEvent)
	if !ok || state.State.Status != stores.StatusSuccess || len(state.State.Steps) != 1 ||
		state.State.Steps[0].Name != "compile" || state.State.Steps[0].Status != stores.StepStatusSuccess {
		t.Fatalf("unexpected state event: %#v", events[2])
	}

	done, ok := events[3].(DoneEvent)
	if !ok || done.Status != stores.StatusSuccess {
		t.Fatalf("unexpected done event: %#v", events[3])
	}
}

func TestStreamLogsStopsWhenCanceled(t *testing.T) {
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
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	execution := stores.Execution{
		ProjectID: project.ID, Workflow: "build", Ref: "main", Status: stores.StatusRunning, CreatedAt: now,
	}
	if err := store.CreateExecution(ctx, &execution); err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	runner := &Runner{Store: store, Workspace: workspace.New(dir, filepath.Join(dir, "logs"))}
	err = runner.StreamLogs(canceled, project.ID, execution.ID, func(StreamEvent) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
