package ci

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
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
