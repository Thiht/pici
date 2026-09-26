package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestBuildSnapshot(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "t")

	mustWrite(t, filepath.Join(dir, "tracked.txt"), "v1")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")

	mustWrite(t, filepath.Join(dir, "tracked.txt"), "v2")
	mustWrite(t, filepath.Join(dir, "untracked.txt"), "new")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "no")

	var buf bytes.Buffer
	if err := buildSnapshot(dir, &buf); err != nil {
		t.Fatal(err)
	}
	names, contents := readTarGz(t, buf.Bytes())

	if _, ok := names["tracked.txt"]; !ok {
		t.Fatal("tracked.txt missing")
	}
	if _, ok := names["untracked.txt"]; !ok {
		t.Fatal("untracked.txt missing")
	}
	if _, ok := names["ignored.txt"]; ok {
		t.Fatal("ignored.txt should be excluded")
	}
	if contents["tracked.txt"] != "v2" {
		t.Fatalf("tracked.txt content = %q, want v2", contents["tracked.txt"])
	}
	if _, ok := names[".git/HEAD"]; !ok {
		t.Fatal(".git/HEAD missing")
	}
	if _, ok := names[".git/config"]; ok {
		t.Fatal(".git/config should be excluded")
	}
}

func TestWorktreeInfo(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@t")
	git(t, dir, "config", "user.name", "t")
	mustWrite(t, filepath.Join(dir, "a"), "a")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")

	ref, sha, err := worktreeInfo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "main" {
		t.Fatalf("ref = %q, want main", ref)
	}
	if len(sha) != 40 {
		t.Fatalf("sha = %q", sha)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTarGz(t *testing.T, data []byte) (map[string]bool, map[string]string) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	contents := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[hdr.Name] = true
		if hdr.Typeflag == tar.TypeReg {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			contents[hdr.Name] = string(b)
		}
	}
	return names, contents
}
