package ci

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"uuid"

	"github.com/Thiht/pici/internal/app/artifacts"
)

func (r *Runner) collectArtifacts(projectID uuid.UUID, execID int64, index int, patterns []string, repoDir string, warn io.Writer) {
	if len(patterns) == 0 {
		return
	}
	if warn == nil {
		warn = io.Discard
	}
	root := r.Workspace.StepArtifacts(projectID, execID, index)
	manifestPath := r.Workspace.StepManifest(projectID, execID, index)
	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintf(warn, "reset artifacts: %v\n", err)
	}
	if err := os.Remove(manifestPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(warn, "reset artifact manifest: %v\n", err)
	}
	entries := []artifacts.Entry{}
	_ = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if !matchesAny(patterns, relSlash) {
			return nil
		}
		dest := filepath.Join(root, filepath.FromSlash(relSlash))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			fmt.Fprintf(warn, "collect artifact %s: %v\n", relSlash, err)
			return nil
		}
		hash, err := artifacts.HashFile(path, dest)
		if err != nil {
			fmt.Fprintf(warn, "collect artifact %s: %v\n", relSlash, err)
			return nil
		}
		entries = append(entries, artifacts.Entry{Path: relSlash, Hash: hash})
		return nil
	})
	if err := artifacts.WriteManifest(manifestPath, entries); err != nil {
		fmt.Fprintf(warn, "write artifact manifest: %v\n", err)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

func cacheBinds(projectID, mountPath string, paths []string) []string {
	var binds []string
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimPrefix(p, "/"))
		if p == "" || p == "." {
			continue
		}
		vol := volumeName(projectID, p)
		binds = append(binds, vol+":"+mountPath+"/"+p)
	}
	return binds
}

func volumeName(projectID, path string) string {
	var b strings.Builder
	b.WriteString(VolumePrefix(projectID))
	for _, r := range strings.ToLower(path) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// VolumePrefix is the name prefix shared by every cross-run cache volume of a
// project. It is also used to find those volumes for the cache view.
func VolumePrefix(projectID string) string {
	return "pici-cache-" + projectID + "-"
}
