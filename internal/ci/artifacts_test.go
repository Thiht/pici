package ci

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/Thiht/pici/internal/workspace"
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
	dir := t.TempDir()
	r := &Runner{Workspace: workspace.New(dir, filepath.Join(dir, "logs"))}
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
	stale := filepath.Join(r.Workspace.StepArtifacts(projectID, 1, 0), "stale.txt")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.collectArtifacts(projectID, 1, 0, []string{"dist/**"}, repoDir, io.Discard)

	got := filepath.Join(r.Workspace.StepArtifacts(projectID, 1, 0), "dist", "app")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected artifact at %s: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(r.Workspace.StepArtifacts(projectID, 1, 0), "other.txt")); err == nil {
		t.Fatal("other.txt should not have been collected")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale artifact should have been removed before collection")
	}

	data, err := os.ReadFile(r.Workspace.StepManifest(projectID, 1, 0))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Version   int `json:"version"`
		Artifacts []struct {
			Path string `json:"path"`
			Hash struct {
				Algorithm string `json:"algorithm"`
				Value     string `json:"value"`
			} `json:"hash"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Version != 1 || len(manifest.Artifacts) != 1 {
		t.Fatalf("unexpected manifest: %s", data)
	}
	entry := manifest.Artifacts[0]
	if entry.Path != "dist/app" || entry.Hash.Algorithm != "sha256" ||
		entry.Hash.Value != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("unexpected manifest entry: %+v", entry)
	}
}
