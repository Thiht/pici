package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"
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

func TestVolumePrefix(t *testing.T) {
	if got := VolumePrefix("abc"); got != "pici-cache-abc-" {
		t.Fatalf("VolumePrefix = %q", got)
	}
	if got := volumeName("abc", ".cache/go"); !strings.HasPrefix(got, VolumePrefix("abc")) {
		t.Fatalf("volumeName %q does not start with the prefix", got)
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

	projectID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	r.collectArtifacts(projectID, 1, 0, []string{"dist/**"}, repoDir)

	got := filepath.Join(r.ArtifactDir(projectID, 1), "000", "dist", "app")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected artifact at %s: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(r.ArtifactDir(projectID, 1), "000", "other.txt")); err == nil {
		t.Fatal("other.txt should not have been collected")
	}
}
