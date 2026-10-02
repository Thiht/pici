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

func TestIsTag(t *testing.T) {
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

	if !IsTag(dir, "v1.0.0") {
		t.Fatal("expected v1.0.0 to be a tag")
	}
	if IsTag(dir, "master") {
		t.Fatal("expected master to not be a tag")
	}
	if IsTag(dir, "nope") {
		t.Fatal("expected unknown ref to not be a tag")
	}
}

func TestCloneChecksOutCommitFromNonDefaultBranch(t *testing.T) {
	src := t.TempDir()
	repo, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}

	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}

	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("feature"),
		Create: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "feature.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("feature.txt"); err != nil {
		t.Fatal(err)
	}
	hash, err := wt.Commit("feature", &git.CommitOptions{Author: sig})
	if err != nil {
		t.Fatal(err)
	}

	if err := wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("master")}); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "clone")
	if err := Clone(context.Background(), CloneConfig{URL: src, Dir: dir, Ref: hash.String()}); err != nil {
		t.Fatalf("clone commit from non-default branch: %v", err)
	}

	cloned, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := cloned.Head()
	if err != nil {
		t.Fatal(err)
	}
	if head.Hash() != hash {
		t.Fatalf("HEAD = %s, want %s", head.Hash(), hash)
	}
}

func TestCloneChecksOutPullRequestRef(t *testing.T) {
	src := t.TempDir()
	repo, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}

	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}

	// The contributor's commit is only reachable through the pull request ref,
	// like a commit coming from a fork.
	if err := wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("fork"), Create: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "fork.txt"), []byte("fork"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("fork.txt"); err != nil {
		t.Fatal(err)
	}
	prHash, err := wt.Commit("fork", &git.CommitOptions{Author: sig})
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("master")}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.RemoveReference(plumbing.NewBranchReferenceName("fork")); err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{"refs/pull/1/head", "refs/merge-requests/1/head"} {
		if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(ref), prHash)); err != nil {
			t.Fatal(err)
		}

		dir := filepath.Join(t.TempDir(), "clone")
		if err := Clone(context.Background(), CloneConfig{URL: src, Dir: dir, Ref: ref}); err != nil {
			t.Fatalf("clone %s: %v", ref, err)
		}

		cloned, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		head, err := cloned.Head()
		if err != nil {
			t.Fatal(err)
		}
		if head.Hash() != prHash {
			t.Fatalf("%s: HEAD = %s, want %s", ref, head.Hash(), prHash)
		}
		content, err := os.ReadFile(filepath.Join(dir, "fork.txt"))
		if err != nil || string(content) != "fork" {
			t.Fatalf("%s: expected the contributor's file, got %q (err=%v)", ref, content, err)
		}
	}
}

func TestCloneUnknownPullRequestRef(t *testing.T) {
	src := t.TempDir()
	repo, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("file.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	err = Clone(context.Background(), CloneConfig{
		URL: src,
		Dir: filepath.Join(t.TempDir(), "clone"),
		Ref: "refs/pull/42/head",
	})
	if err == nil {
		t.Fatal("expected an error for a missing pull request ref")
	}
}
