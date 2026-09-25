package ci

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func (r *Runner) ArtifactDir(execID string) string {
	return filepath.Join(r.WorkspaceDir, "artifacts", execID)
}

func (r *Runner) collectArtifacts(execID string, index int, patterns []string, repoDir string) {
	if len(patterns) == 0 {
		return
	}
	destRoot := filepath.Join(r.ArtifactDir(execID), fmt.Sprintf("%03d", index))
	_ = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		for _, p := range patterns {
			if matchGlob(p, relSlash) {
				dest := filepath.Join(destRoot, filepath.FromSlash(relSlash))
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err == nil {
					_ = copyFile(path, dest)
				}
				break
			}
		}
		return nil
	})
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
		out.Close()
		return err
	}
	return out.Close()
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
	b.WriteString("pici-cache-")
	b.WriteString(projectID)
	b.WriteString("-")
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
