package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestListRefs(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatal(err)
	}
	hash, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTag("v1.0.0", hash, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("dev"), hash)); err != nil {
		t.Fatal(err)
	}

	refs, err := ListRefs(context.Background(), dir, Auth{Type: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(refs.Branches, "master") || !slices.Contains(refs.Branches, "dev") {
		t.Fatalf("branches = %v, want master and dev", refs.Branches)
	}
	if !slices.Contains(refs.Tags, "v1.0.0") {
		t.Fatalf("tags = %v, want v1.0.0", refs.Tags)
	}
}
