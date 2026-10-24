package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Thiht/pici/internal/stores"
)

const helloHash = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestArtifactListAndDownloadETag(t *testing.T) {
	s, store, ws, projectID := newExecServer(t, 0)

	execution := stores.Execution{
		ProjectID: projectID,
		Workflow:  "build",
		Ref:       "main",
		Steps:     stores.Steps{{Name: "build", Status: stores.StepStatusSuccess}},
	}
	if err := store.CreateExecution(t.Context(), &execution); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(ws, "artifacts", projectID.String(), strconv.FormatInt(execution.ID, 10))
	artifactPath := filepath.Join(root, "000", "dist", "app")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"artifacts":[{"path":"dist/app","hash":{"algorithm":"sha256","value":"` + helloHash + `"}}]}`
	if err := os.WriteFile(filepath.Join(root, "000.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	base := "/api/projects/" + projectID.String() + "/executions/" + strconv.FormatInt(execution.ExecutionID, 10)

	rr := serveAuthed(t, s, httptest.NewRequest(http.MethodGet, base+"/artifacts", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"hash":{"algorithm":"sha256","value":"`+helloHash+`"}`) {
		t.Fatalf("list: missing hash in %s", body)
	}

	rr = serveAuthed(t, s, httptest.NewRequest(http.MethodGet, base+"/artifacts/build/dist/app", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "hello" {
		t.Fatalf("download: expected 200 hello, got %d: %s", rr.Code, rr.Body.String())
	}
	if etag := rr.Header().Get("ETag"); etag != `"sha256-`+helloHash+`"` {
		t.Fatalf("download: unexpected ETag %q", etag)
	}

	req := httptest.NewRequest(http.MethodGet, base+"/artifacts/build/dist/app", nil)
	req.Header.Set("If-None-Match", `"sha256-`+helloHash+`"`)
	rr = serveAuthed(t, s, req)
	if rr.Code != http.StatusNotModified {
		t.Fatalf("download: expected 304, got %d: %s", rr.Code, rr.Body.String())
	}

	if err := os.Remove(filepath.Join(root, "000.json")); err != nil {
		t.Fatal(err)
	}
	rr = serveAuthed(t, s, httptest.NewRequest(http.MethodGet, base+"/artifacts", nil))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), `"hash"`) {
		t.Fatalf("list without manifest: expected no hash, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestArtifactDownloadNormalizesPathForETag(t *testing.T) {
	s, store, ws, projectID := newExecServer(t, 0)

	execution := stores.Execution{
		ProjectID: projectID,
		Workflow:  "build",
		Ref:       "main",
		Steps:     stores.Steps{{Name: "build", Status: stores.StepStatusSuccess}},
	}
	if err := store.CreateExecution(t.Context(), &execution); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(ws, "artifacts", projectID.String(), strconv.FormatInt(execution.ID, 10))
	artifactPath := filepath.Join(root, "000", "dist", "app")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"artifacts":[{"path":"dist/app","hash":{"algorithm":"sha256","value":"` + helloHash + `"}}]}`
	if err := os.WriteFile(filepath.Join(root, "000.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"dist//app", "./dist/app"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("id", projectID.String())
		req.SetPathValue("executionID", strconv.FormatInt(execution.ExecutionID, 10))
		req.SetPathValue("step", "build")
		req.SetPathValue("path", path)
		rr := httptest.NewRecorder()
		s.artifacts.Download(rr, req)
		if rr.Code != http.StatusOK || rr.Body.String() != "hello" {
			t.Fatalf("%s: expected 200 hello, got %d: %s", path, rr.Code, rr.Body.String())
		}
		if etag := rr.Header().Get("ETag"); etag != `"sha256-`+helloHash+`"` {
			t.Fatalf("%s: unexpected ETag %q", path, etag)
		}
	}
}
