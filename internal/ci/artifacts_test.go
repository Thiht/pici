package ci

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
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
	stale := filepath.Join(r.ArtifactDir(projectID, 1), "000", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.collectArtifacts(projectID, 1, 0, []string{"dist/**"}, repoDir, io.Discard)

	got := filepath.Join(r.ArtifactDir(projectID, 1), "000", "dist", "app")
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("expected artifact at %s: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(r.ArtifactDir(projectID, 1), "000", "other.txt")); err == nil {
		t.Fatal("other.txt should not have been collected")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale artifact should have been removed before collection")
	}

	data, err := os.ReadFile(r.artifactManifestPath(projectID, 1, 0))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest artifactManifest
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

	execution := stores.Execution{ProjectID: projectID, ID: 1, Steps: stores.Steps{{Name: "build"}}}
	artifacts := r.ListArtifacts(execution)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Step != "build" || artifacts[0].Path != "dist/app" || artifacts[0].Hash == nil {
		t.Fatalf("unexpected artifact: %+v", artifacts[0])
	}
	if etag := r.ArtifactETag(execution, 0, "dist/app"); etag != `"sha256-2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"` {
		t.Fatalf("unexpected ETag: %q", etag)
	}

	if err := os.Remove(r.artifactManifestPath(projectID, 1, 0)); err != nil {
		t.Fatal(err)
	}
	artifacts = r.ListArtifacts(execution)
	if len(artifacts) != 1 || artifacts[0].Hash != nil {
		t.Fatalf("artifact without manifest should still be listed without a hash: %+v", artifacts)
	}
	if etag := r.ArtifactETag(execution, 0, "dist/app"); etag != "" {
		t.Fatalf("expected no ETag without a manifest, got %q", etag)
	}
}

func TestListArtifactsRejectsInvalidManifests(t *testing.T) {
	r := &Runner{WorkspaceDir: t.TempDir()}
	projectID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	const execID int64 = 2
	stepDir := filepath.Join(r.ArtifactDir(projectID, execID), "000", "dist")
	if err := os.MkdirAll(stepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stepDir, "app"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	execution := stores.Execution{ProjectID: projectID, ID: execID, Steps: stores.Steps{{Name: "build"}}}
	manifestPath := r.artifactManifestPath(projectID, execID, 0)

	cases := []struct {
		name    string
		content string
	}{
		{"corrupt", `{`},
		{"unsupported version", `{"version":2,"artifacts":[{"path":"dist/app","hash":{"algorithm":"sha256","value":"deadbeef"}}]}`},
		{"unsupported algorithm", `{"version":1,"artifacts":[{"path":"dist/app","hash":{"algorithm":"md5","value":"deadbeef"}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(manifestPath, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			artifacts := r.ListArtifacts(execution)
			if len(artifacts) != 1 || artifacts[0].Hash != nil {
				t.Fatalf("expected the artifact without a hash, got %+v", artifacts)
			}
			if etag := r.ArtifactETag(execution, 0, "dist/app"); etag != "" {
				t.Fatalf("expected no ETag, got %q", etag)
			}
		})
	}
}
