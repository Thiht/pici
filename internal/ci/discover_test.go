package ci

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/Thiht/pici/internal/stores"
)

func TestDiscoverProjectWorkflowsIsIdempotent(t *testing.T) {
	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, ".ci", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".ci", "build", "ci.yml"), []byte("steps:\n  - name: test\n    run: echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(".ci"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}

	project := stores.Project{RepoURL: repoDir, AuthType: stores.AuthTypeNone}
	cloneDir := filepath.Join(t.TempDir(), "_discovery")

	first, err := DiscoverProjectWorkflows(context.Background(), project, cloneDir, "master")
	if err != nil {
		t.Fatalf("first discovery: %v", err)
	}
	if len(first) != 1 || first[0] != "build" {
		t.Fatalf("unexpected workflows: %v", first)
	}

	second, err := DiscoverProjectWorkflows(context.Background(), project, cloneDir, "master")
	if err != nil {
		t.Fatalf("second discovery (reused dir): %v", err)
	}
	if len(second) != 1 || second[0] != "build" {
		t.Fatalf("unexpected workflows on second run: %v", second)
	}
}
