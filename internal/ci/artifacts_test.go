package ci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheBinds(t *testing.T) {
	binds := cacheBinds("proj", "/workspace", []string{"node_modules", ".cache/go", "/abs/path"})
	want := []string{
		"pici-cache-proj-node_modules:/workspace/node_modules",
		"pici-cache-proj-.cache-go:/workspace/.cache/go",
		"pici-cache-proj-abs-path:/workspace/abs/path",
	}
	if len(binds) != len(want) {
		t.Fatalf("got %v, want %v", binds, want)
	}
	for i := range want {
		if binds[i] != want[i] {
			t.Fatalf("binds[%d] = %q, want %q", i, binds[i], want[i])
		}
	}
}

func TestCollectArtifacts(t *testing.T) {
	r := &Runner{WorkspaceDir: t.TempDir()}
	repoDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(repoDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "dist", "app"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.collectArtifacts("exec1", 0, []string{"dist/**"}, repoDir)

	got := filepath.Join(r.ArtifactDir("exec1"), "000", "dist", "app")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected artifact at %s: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(r.ArtifactDir("exec1"), "000", "other.txt")); err == nil {
		t.Fatal("other.txt should not have been collected")
	}
}
