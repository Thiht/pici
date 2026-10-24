package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"uuid"
)

func TestSourceRoundTrip(t *testing.T) {
	ws := New(t.TempDir(), t.TempDir())
	projectID := uuid.New()
	for _, step := range []string{"install", "test:unit", "go test ./..."} {
		path := ws.StepLog(projectID, 1, 2, step)
		if got, want := LogSource(path), Source(step); got != want {
			t.Errorf("LogSource(%q) = %q, want %q", path, got, want)
		}
	}
	if got := LogSource(ws.SetupLog(projectID, 1)); got != "setup" {
		t.Errorf("LogSource(setup log) = %q", got)
	}
}

func TestLogPathsOrder(t *testing.T) {
	ws := New(t.TempDir(), t.TempDir())
	projectID := uuid.New()

	for _, index := range []int{1, 0} {
		path := ws.StepLog(projectID, 3, index, []string{"install", "test"}[index])
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("log"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	paths := ws.LogPaths(projectID, 3)
	if len(paths) != 2 {
		t.Fatalf("expected the two step logs, got %v", paths)
	}
	if filepath.Base(paths[0]) != "000_install.log" || filepath.Base(paths[1]) != "001_test.log" {
		t.Fatalf("step logs should be ordered by index, got %v", paths)
	}
}
