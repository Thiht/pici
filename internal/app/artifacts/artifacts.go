// Package artifacts owns what a Step collected: the manifest that records each
// artifact's hash, the listing the UI and the API show, and the bytes a
// download serves.
package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

// Hash is the digest of an artifact's bytes.
type Hash struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// Artifact is a file collected by a step's artifacts globs.
type Artifact struct {
	Step string `json:"step"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	Hash *Hash  `json:"hash,omitempty"`
}

// File is an artifact opened for download.
type File struct {
	io.ReadSeekCloser
	Info fs.FileInfo
	ETag string
}

// Entry is one artifact as recorded in a Step's manifest.
type Entry struct {
	Path string `json:"path"`
	Hash Hash   `json:"hash"`
}

type manifest struct {
	Version   int     `json:"version"`
	Artifacts []Entry `json:"artifacts"`
}

const manifestVersion = 1

// Module lists, opens and records artifacts.
type Module struct {
	workspace *workspace.Workspace
}

func New(ws *workspace.Workspace) *Module {
	return &Module{workspace: ws}
}

// List returns every artifact collected for an Execution, ordered by Step. An
// artifact missing from its Step's manifest is still listed, without a Hash.
func (m *Module) List(exec stores.Execution) []Artifact {
	root := m.workspace.ArtifactDir(exec.ProjectID, exec.ID)
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
		hashes := readManifest(m.workspace.StepManifest(exec.ProjectID, exec.ID, index))
		step := dir.Name()
		if index < len(exec.Steps) {
			step = exec.Steps[index].Name
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
			var size int64
			if info, err := d.Info(); err == nil {
				size = info.Size()
			}
			artifact := Artifact{Step: step, Path: filepath.ToSlash(rel), Size: size}
			if hash, ok := hashes[artifact.Path]; ok {
				artifact.Hash = &hash
			}
			out = append(out, artifact)
			return nil
		})
	}
	return out
}

// Open opens one artifact of a Step. It reports app.ErrNotFound when the Step
// or the file does not exist, and app.ErrInvalid when path escapes the Step's
// directory.
func (m *Module) Open(exec stores.Execution, step, path string) (File, error) {
	index := -1
	for i, s := range exec.Steps {
		if s.Name == step {
			index = i
			break
		}
	}
	if index < 0 {
		return File{}, app.ErrNotFound
	}
	dir := m.workspace.StepArtifacts(exec.ProjectID, exec.ID, index)
	full := filepath.Clean(filepath.Join(dir, filepath.FromSlash(path)))
	if full != dir && !strings.HasPrefix(full, dir+string(filepath.Separator)) {
		return File{}, app.ErrInvalid
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		return File{}, app.ErrNotFound
	}
	f, err := os.Open(full)
	if err != nil {
		return File{}, app.ErrNotFound
	}
	rel, err := filepath.Rel(dir, full)
	if err != nil {
		_ = f.Close()
		return File{}, app.ErrInvalid
	}
	var etag string
	if hash, ok := readManifest(m.workspace.StepManifest(exec.ProjectID, exec.ID, index))[filepath.ToSlash(rel)]; ok && hash.Algorithm == "sha256" {
		etag = `"sha256-` + hash.Value + `"`
	}
	return File{ReadSeekCloser: f, Info: info, ETag: etag}, nil
}

// WriteManifest records the artifacts a Step collected. It is written
// atomically so a listing never sees half a manifest.
func WriteManifest(path string, entries []Entry) error {
	root := filepath.Dir(path)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(manifest{Version: manifestVersion, Artifacts: entries})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ".manifest-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
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

// HashFile copies src to dst and hashes the bytes as they stream through.
func HashFile(src, dst string) (Hash, error) {
	hasher := sha256.New()
	in, err := os.Open(src)
	if err != nil {
		return Hash{}, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return Hash{}, err
	}
	if _, err := io.Copy(io.MultiWriter(out, hasher), in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return Hash{}, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return Hash{}, err
	}
	return Hash{Algorithm: "sha256", Value: hex.EncodeToString(hasher.Sum(nil))}, nil
}

func readManifest(path string) map[string]Hash {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("read artifact manifest", "path", path, "error", err)
		}
		return nil
	}
	var decoded manifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		slog.Warn("decode artifact manifest", "path", path, "error", err)
		return nil
	}
	if decoded.Version != manifestVersion {
		slog.Warn("unsupported artifact manifest version", "path", path, "version", decoded.Version)
		return nil
	}
	hashes := make(map[string]Hash, len(decoded.Artifacts))
	for _, entry := range decoded.Artifacts {
		if entry.Hash.Algorithm == "sha256" && entry.Hash.Value != "" {
			hashes[entry.Path] = entry.Hash
		}
	}
	return hashes
}
