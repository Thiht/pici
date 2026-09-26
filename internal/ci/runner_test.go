package ci

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/Thiht/pici/internal/stores"
)

func TestImagePrefix(t *testing.T) {
	if got := imageTag("abc", "build"); got != "pici/abc-build" {
		t.Fatalf("imageTag = %q", got)
	}
	if got := ImagePrefix("abc"); got != "pici/abc-" {
		t.Fatalf("ImagePrefix = %q", got)
	}
}

func TestResolveVersion(t *testing.T) {
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("a"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	hash, err := wt.Commit("one", &gogit.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTag("v1.2.3", hash, nil); err != nil {
		t.Fatal(err)
	}

	if got := resolveVersion(dir, "v1.2.3", hash.String()); got != "v1.2.3" {
		t.Fatalf("tag ref: got %q, want v1.2.3", got)
	}
	if got := resolveVersion(dir, "main", hash.String()); got != hash.String()[:7] {
		t.Fatalf("branch ref: got %q, want %q", got, hash.String()[:7])
	}
}

func TestBuildEnvSnapshotSource(t *testing.T) {
	env := buildEnv(stores.Project{}, stores.Execution{Source: stores.SourceSnapshot}, "/workspace", "abcdef12345", "abcdef1")
	if !slices.Contains(env, "PICI_SOURCE=snapshot") {
		t.Fatalf("missing PICI_SOURCE=snapshot in %v", env)
	}
}

func TestMaterializeSnapshot(t *testing.T) {
	projectID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	snapshotID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	ws := t.TempDir()
	r := &Runner{WorkspaceDir: ws, MaxSnapshotSize: 1 << 20}

	dir := filepath.Join(ws, "uploads", projectID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, filepath.Join(dir, snapshotID.String()+".tar.gz"), map[string]string{
		".git/HEAD":        "ref: refs/heads/main\n",
		".ci/build/ci.yml": "steps:\n",
	})

	repoDir := filepath.Join(ws, projectID.String(), "1")
	project := stores.Project{ID: projectID, RepoURL: "https://example.com/acme/demo.git"}
	exec := stores.Execution{Source: stores.SourceSnapshot, SnapshotID: &snapshotID}
	if err := r.materializeSource(context.Background(), project, exec, repoDir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(repoDir, ".ci", "build", "ci.yml")); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join(repoDir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), project.RepoURL) {
		t.Fatalf("config missing origin url: %s", cfg)
	}
	if !strings.Contains(string(cfg), `[remote "origin"]`) {
		t.Fatalf("config missing origin section: %s", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, snapshotID.String()+".tar.gz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected archive to be removed after extraction")
	}
}

func TestMaterializeSnapshotKeepsArchiveOnFailure(t *testing.T) {
	projectID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	snapshotID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	ws := t.TempDir()
	r := &Runner{WorkspaceDir: ws, MaxSnapshotSize: 1 << 20}

	dir := filepath.Join(ws, "uploads", projectID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, snapshotID.String()+".tar.gz")
	if err := os.WriteFile(archivePath, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}

	repoDir := filepath.Join(ws, projectID.String(), "1")
	project := stores.Project{ID: projectID, RepoURL: "https://example.com/acme/demo.git"}
	exec := stores.Execution{Source: stores.SourceSnapshot, SnapshotID: &snapshotID}
	if err := r.materializeSource(context.Background(), project, exec, repoDir); err == nil {
		t.Fatal("expected materialize to fail")
	}

	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive should be kept when materialization fails: %v", err)
	}
}

func writeSnapshot(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}
