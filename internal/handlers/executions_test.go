package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
)

func newExecServer(t *testing.T, maxSnapshotSize int64) (*Handler, stores.Store, string, uuid.UUID) {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	projectID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	if _, err := store.CreateProject(t.Context(), stores.Project{
		ID:            projectID,
		Name:          "demo",
		RepoURL:       "https://example.com/acme/demo.git",
		DefaultBranch: "main",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	ws := t.TempDir()
	s := New(store, &ci.Runner{Store: store, WorkspaceDir: ws}, nil, ws, "/workspace", maxSnapshotSize, "", "dev")
	return s, store, ws, projectID
}

func snapshotRequest(t *testing.T, projectID uuid.UUID, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("workflow", "build")
	_ = mw.WriteField("ref", "main")
	part, err := mw.CreateFormFile("snapshot", "snapshot.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID.String()+"/executions", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestCreateSnapshotExecution(t *testing.T) {
	s, store, ws, projectID := newExecServer(t, 1<<20)

	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, snapshotRequest(t, projectID, []byte("fake archive")))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	execs, err := store.ListExecutions(t.Context(), projectID, 10)
	if err != nil || len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(execs), err)
	}
	if execs[0].Source != stores.SourceSnapshot || execs[0].SnapshotID == nil {
		t.Fatalf("unexpected execution: %+v", execs[0])
	}
	path := filepath.Join(ws, "uploads", projectID.String(), execs[0].SnapshotID.String()+".tar.gz")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archive not stored: %v", err)
	}
}

func TestCreateSnapshotTooLarge(t *testing.T) {
	s, _, _, projectID := newExecServer(t, 8)

	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, snapshotRequest(t, projectID, bytes.Repeat([]byte("a"), 4096)))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRebuildSnapshotRejected(t *testing.T) {
	s, store, _, projectID := newExecServer(t, 1<<20)

	snapshotID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	e := stores.Execution{ProjectID: projectID, Workflow: "build", Status: stores.StatusSuccess, Source: stores.SourceSnapshot, SnapshotID: &snapshotID}
	if err := store.CreateExecution(t.Context(), &e); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID.String()+"/executions/"+itoa(e.ID)+"/rebuild", nil)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestRetryExecution(t *testing.T) {
	s, store, ws, projectID := newExecServer(t, 1<<20)

	previous := stores.Execution{
		ProjectID: projectID, Workflow: "build", Ref: "main", Status: stores.StatusFailed, Source: stores.SourceGit, CreatedAt: time.Now(),
		Steps: stores.Steps{
			{Name: "install", Status: stores.StepStatusSuccess},
			{Name: "test", Status: stores.StepStatusFailed},
		},
	}
	if err := store.CreateExecution(t.Context(), &previous); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, projectID.String(), itoa(previous.ID)), 0o755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID.String()+"/executions/"+itoa(previous.ID)+"/retry", nil)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	execs, err := store.ListExecutions(t.Context(), projectID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var retried *stores.Execution
	for i := range execs {
		if execs[i].Trigger == stores.TriggerRetry {
			retried = &execs[i]
		}
	}
	if retried == nil || retried.ParentID == nil || *retried.ParentID != previous.ID {
		t.Fatalf("retry not created: %+v", execs)
	}
}

func TestRetrySnapshotRejected(t *testing.T) {
	s, store, _, projectID := newExecServer(t, 1<<20)

	snapshotID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	e := stores.Execution{
		ProjectID: projectID, Workflow: "build", Status: stores.StatusFailed, Source: stores.SourceSnapshot, SnapshotID: &snapshotID, CreatedAt: time.Now(),
		Steps: stores.Steps{{Name: "test", Status: stores.StepStatusFailed}},
	}
	if err := store.CreateExecution(t.Context(), &e); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID.String()+"/executions/"+itoa(e.ID)+"/retry", nil)
	rr := httptest.NewRecorder()
	s.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}
