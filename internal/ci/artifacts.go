package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
)

// ArtifactHash is the digest of an artifact's bytes.
type ArtifactHash struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// Artifact is a file collected by a step's artifacts globs.
type Artifact struct {
	Step string        `json:"step"`
	Path string        `json:"path"`
	Size int64         `json:"size"`
	Hash *ArtifactHash `json:"hash,omitempty"`
}

type artifactManifest struct {
	Version   int             `json:"version"`
	Artifacts []manifestEntry `json:"artifacts"`
}

type manifestEntry struct {
	Path string       `json:"path"`
	Hash ArtifactHash `json:"hash"`
}

func (r *Runner) ArtifactDir(projectID uuid.UUID, execID int64) string {
	return filepath.Join(r.WorkspaceDir, "artifacts", projectID.String(), strconv.FormatInt(execID, 10))
}

func (r *Runner) artifactManifestPath(projectID uuid.UUID, execID int64, index int) string {
	return filepath.Join(r.ArtifactDir(projectID, execID), fmt.Sprintf("%03d.json", index))
}

func (r *Runner) collectArtifacts(projectID uuid.UUID, execID int64, index int, patterns []string, repoDir string, warn io.Writer) {
	if len(patterns) == 0 {
		return
	}
	if warn == nil {
		warn = io.Discard
	}
	root := r.ArtifactDir(projectID, execID)
	destRoot := filepath.Join(root, fmt.Sprintf("%03d", index))
	manifestPath := r.artifactManifestPath(projectID, execID, index)
	if err := os.RemoveAll(destRoot); err != nil {
		fmt.Fprintf(warn, "reset artifacts: %v\n", err)
	}
	if err := os.Remove(manifestPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(warn, "reset artifact manifest: %v\n", err)
	}
	entries := []manifestEntry{}
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
		dest := filepath.Join(destRoot, filepath.FromSlash(relSlash))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			fmt.Fprintf(warn, "collect artifact %s: %v\n", relSlash, err)
			return nil
		}
		hash, err := copyArtifact(path, dest)
		if err != nil {
			fmt.Fprintf(warn, "collect artifact %s: %v\n", relSlash, err)
			return nil
		}
		entries = append(entries, manifestEntry{Path: relSlash, Hash: hash})
		return nil
	})
	if err := writeArtifactManifest(manifestPath, entries); err != nil {
		fmt.Fprintf(warn, "write artifact manifest: %v\n", err)
	}
}

// copyArtifact copies src to dst and hashes the bytes as they stream through.
func copyArtifact(src, dst string) (ArtifactHash, error) {
	h := sha256.New()
	if err := copyInto(src, dst, h); err != nil {
		return ArtifactHash{}, err
	}
	return ArtifactHash{Algorithm: "sha256", Value: hex.EncodeToString(h.Sum(nil))}, nil
}

func copyInto(src, dst string, hasher io.Writer) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.MultiWriter(out, hasher), in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

func writeArtifactManifest(path string, entries []manifestEntry) error {
	root := filepath.Dir(path)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(artifactManifest{Version: 1, Artifacts: entries})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".manifest-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// ListArtifacts returns the artifacts collected for an execution. Artifacts
// missing from their step manifest are still listed, without a hash.
func (r *Runner) ListArtifacts(execution stores.Execution) []Artifact {
	root := r.ArtifactDir(execution.ProjectID, execution.ID)
	dirs, err := os.ReadDir(root)
	if err != nil {
		return []Artifact{}
	}
	out := []Artifact{}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		index, err := strconv.Atoi(dir.Name())
		if err != nil || index < 0 {
			continue
		}
		hashes := readArtifactManifest(r.artifactManifestPath(execution.ProjectID, execution.ID, index))
		step := fmt.Sprintf("%03d", index)
		if index < len(execution.Steps) {
			step = execution.Steps[index].Name
		}
		stepDir := filepath.Join(root, dir.Name())
		_ = filepath.WalkDir(stepDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(stepDir, path)
			if err != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			var size int64
			if info, err := d.Info(); err == nil {
				size = info.Size()
			}
			item := Artifact{Step: step, Path: relSlash, Size: size}
			if hash, ok := hashes[relSlash]; ok {
				item.Hash = &hash
			}
			out = append(out, item)
			return nil
		})
	}
	return out
}

// ArtifactETag returns the ETag of a collected artifact, or an empty string
// when its hash is unknown.
func (r *Runner) ArtifactETag(execution stores.Execution, index int, path string) string {
	hashes := readArtifactManifest(r.artifactManifestPath(execution.ProjectID, execution.ID, index))
	hash, ok := hashes[path]
	if !ok || hash.Algorithm != "sha256" {
		return ""
	}
	return `"sha256-` + hash.Value + `"`
}

func readArtifactManifest(path string) map[string]ArtifactHash {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("read artifact manifest", "path", path, "error", err)
		}
		return nil
	}
	var manifest artifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		slog.Warn("decode artifact manifest", "path", path, "error", err)
		return nil
	}
	if manifest.Version != 1 {
		slog.Warn("unsupported artifact manifest version", "path", path, "version", manifest.Version)
		return nil
	}
	hashes := make(map[string]ArtifactHash, len(manifest.Artifacts))
	for _, entry := range manifest.Artifacts {
		if entry.Hash.Algorithm == "sha256" && entry.Hash.Value != "" {
			hashes[entry.Path] = entry.Hash
		}
	}
	return hashes
}

func copyFile(src, dst string) error {
	return copyInto(src, dst, io.Discard)
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
