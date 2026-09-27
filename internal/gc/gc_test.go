package gc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
