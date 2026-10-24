package artifacts

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

const (
	projectID = "11111111-1111-1111-1111-111111111111"
	execID    = int64(1)
	appHash   = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
)

func newTestModule(t *testing.T) (*Module, *workspace.Workspace) {
	t.Helper()
	dir := t.TempDir()
	ws := workspace.New(dir, filepath.Join(dir, "logs"))
	return New(ws), ws
}

func execution() stores.Execution {
	return stores.Execution{
		ProjectID: uuid.MustParse(projectID),
		ID:        execID,
		Steps:     stores.Steps{{Name: "build"}},
	}
}

// collect writes one artifact and its manifest the way a Step would.
func collect(t *testing.T, ws *workspace.Workspace) {
	t.Helper()
	exec := execution()
	stepDir := ws.StepArtifacts(exec.ProjectID, exec.ID, 0)
	if err := os.MkdirAll(filepath.Join(stepDir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stepDir, "dist", "app"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteManifest(ws.StepManifest(exec.ProjectID, exec.ID, 0), []Entry{
		{Path: "dist/app", Hash: Hash{Algorithm: "sha256", Value: appHash}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestList(t *testing.T) {
	module, ws := newTestModule(t)
	collect(t, ws)

	got := module.List(execution())
	if len(got) != 1 {
		t.Fatalf("expected 1 artifact, got %d: %+v", len(got), got)
	}
	artifact := got[0]
	if artifact.Step != "build" || artifact.Path != "dist/app" || artifact.Size != 5 {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
	if artifact.Hash == nil || artifact.Hash.Value != appHash {
		t.Fatalf("expected the manifest hash, got %+v", artifact.Hash)
	}
}

func TestListWithoutManifest(t *testing.T) {
	module, ws := newTestModule(t)
	collect(t, ws)
	if err := os.Remove(ws.StepManifest(execution().ProjectID, execID, 0)); err != nil {
		t.Fatal(err)
	}

	got := module.List(execution())
	if len(got) != 1 || got[0].Hash != nil {
		t.Fatalf("artifact without a manifest should still be listed without a hash: %+v", got)
	}
}

func TestListRejectsInvalidManifests(t *testing.T) {
	cases := map[string]string{
		"corrupt":               `{`,
		"unsupported version":   `{"version":2,"artifacts":[{"path":"dist/app","hash":{"algorithm":"sha256","value":"deadbeef"}}]}`,
		"unsupported algorithm": `{"version":1,"artifacts":[{"path":"dist/app","hash":{"algorithm":"md5","value":"deadbeef"}}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			module, ws := newTestModule(t)
			collect(t, ws)
			if err := os.WriteFile(ws.StepManifest(execution().ProjectID, execID, 0), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			got := module.List(execution())
			if len(got) != 1 || got[0].Hash != nil {
				t.Fatalf("expected the artifact without a hash, got %+v", got)
			}
			file, err := module.Open(execution(), "build", "dist/app")
			if err != nil {
				t.Fatal(err)
			}
			_ = file.Close()
			if file.ETag != "" {
				t.Fatalf("expected no ETag, got %q", file.ETag)
			}
		})
	}
}

func TestOpen(t *testing.T) {
	module, ws := newTestModule(t)
	collect(t, ws)

	file, err := module.Open(execution(), "build", "dist/app")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if file.ETag != `"sha256-`+appHash+`"` {
		t.Fatalf("unexpected ETag: %q", file.ETag)
	}
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "hello" {
		t.Fatalf("read %q, %v", data, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("artifact should be seekable: %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	module, ws := newTestModule(t)
	collect(t, ws)

	if _, err := module.Open(execution(), "build", "../../escape"); !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("expected ErrInvalid for a traversal, got %v", err)
	}
	if _, err := module.Open(execution(), "build", "dist/missing"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for a missing file, got %v", err)
	}
	if _, err := module.Open(execution(), "test", "dist/app"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown step, got %v", err)
	}
}
