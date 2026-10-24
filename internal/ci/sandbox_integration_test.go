//go:build docker

package ci

import (
	"context"
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

// TestRunDockerWorkflowAgainstDocker runs a whole execution whose step drives
// the sandbox daemon, against a real Docker daemon. Run it with
// `go test -tags=docker ./internal/ci`.
func TestRunDockerWorkflowAgainstDocker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	engine, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	if err := engine.Ping(ctx); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

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
		".ci/build/ci.yml": "image: docker:28.5.2-cli\n" +
			"steps:\n" +
			"  - name: nested\n" +
			"    run: docker run --rm busybox echo nested-ok\n" +
			"    docker: true\n" +
			"    timeout: 5m\n",
	})

	runner := &Runner{
		Store: store, Engine: engine, Workspace: ws, MountPath: "/workspace",
		PublicBaseURL: "https://pici.test", DefaultStepTimeout: 5 * time.Minute, MaxSnapshotSize: 1 << 20,
	}
	runner.run(ctx, execution)

	got, err := store.GetAttempt(ctx, project.ID, execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != stores.StatusSuccess {
		t.Fatalf("status = %q (error %q)", got.Status, got.Error)
	}
	stepLog, err := os.ReadFile(ws.StepLog(project.ID, execution.ID, 0, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stepLog), "nested-ok") {
		t.Fatalf("step log = %q", stepLog)
	}

	sandboxes, err := engine.Sandboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, sb := range sandboxes {
		if sb.ProjectID == project.ID.String() {
			t.Fatalf("the execution left a sandbox behind: %+v", sb)
		}
	}
}
