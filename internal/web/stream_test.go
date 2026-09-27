package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func TestExecutionStream(t *testing.T) {
	dir := t.TempDir()
	store, err := stores.Open("sqlite", filepath.Join(dir, "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	projectID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	if _, err := store.CreateProject(t.Context(), stores.Project{
		ID: projectID, Name: "demo", RepoURL: "https://github.com/acme/demo.git",
		DefaultBranch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	exec := stores.Execution{
		ProjectID: projectID, Workflow: "build", Ref: "main",
		Status: stores.StatusSuccess, CreatedAt: time.Now(), FinishedAt: new(time.Now()),
		Steps: stores.Steps{{Name: "compile", Status: stores.StepStatusSuccess}},
	}
	if err := store.CreateExecution(t.Context(), &exec); err != nil {
		t.Fatal(err)
	}

	runner := &ci.Runner{Store: store, LogsDir: filepath.Join(dir, "logs")}
	if err := os.MkdirAll(filepath.Dir(runner.StepLogPath(projectID, exec.ID, 0, "compile")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner.SetupLogPath(projectID, exec.ID), []byte("cloning\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runner.StepLogPath(projectID, exec.ID, 0, "compile"), []byte("building\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(store, runner, dir, "", "dev")
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/projects/" + projectID.String() + "/executions/" + strconv.FormatInt(exec.ID, 10) + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected event stream, got %q", ct)
	}
	out := body(t, resp)
	for _, want := range []string{
		"event: setup\ndata: cloning",
		"event: compile\ndata: building",
		`event: state` + "\n" + `data: {"status":"success","steps":[{"name":"compile","status":"success","exit_code":0}]}`,
		"event: done\ndata: success",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in stream, got:\n%s", want, out)
		}
	}
}
