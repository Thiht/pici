package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func worktreeInfo(dir string) (ref, sha string, err error) {
	out, err := runGit(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("%s is not a git worktree: %w", dir, err)
	}
	sha = strings.TrimSpace(out)
	if branch, err := runGit(dir, "symbolic-ref", "--short", "HEAD"); err == nil {
		if ref = strings.TrimSpace(branch); ref == "" && len(sha) >= 7 {
			ref = sha[:7]
		}
	} else if len(sha) >= 7 {
		ref = sha[:7]
	}
	return ref, sha, nil
}

func buildSnapshot(dir string, w io.Writer) error {
	out, err := runGit(dir, "ls-files", "-c", "-o", "--exclude-standard", "-z")
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		if err := addWorktreeFile(tw, dir, rel); err != nil {
			return err
		}
	}
	if err := addGitDir(tw, dir); err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func addWorktreeFile(tw *tar.Writer, dir, rel string) error {
	full := filepath.Join(dir, filepath.FromSlash(rel))
	fi, err := os.Lstat(full)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(full)
		if err != nil {
			return err
		}
		return tw.WriteHeader(&tar.Header{Name: rel, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777})
	case fi.Mode().IsRegular():
		f, err := os.Open(full)
		if err != nil {
			return err
		}
		defer f.Close()
		hdr := &tar.Header{Name: rel, Typeflag: tar.TypeReg, Mode: int64(fi.Mode().Perm()), Size: fi.Size(), ModTime: fi.ModTime()}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	default:
		return nil
	}
}

func addGitDir(tw *tar.Writer, dir string) error {
	root := filepath.Join(dir, ".git")
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" {
			return nil
		}
		excluded := rel == ".git/config" ||
			rel == ".git/hooks" || strings.HasPrefix(rel, ".git/hooks/") ||
			rel == ".git/logs" || strings.HasPrefix(rel, ".git/logs/")
		if excluded {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: rel + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		}
		return addWorktreeFile(tw, dir, rel)
	})
}
