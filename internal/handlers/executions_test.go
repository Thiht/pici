package handlers

import (
	"bytes"
	"encoding/json"
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
	"github.com/Thiht/pici/internal/workspace"
)

func newExecServer(t *testing.T, maxSnapshotSize int64) (*Handler, stores.Store, string, uuid.UUID) {
	t.Helper()
	store := newTestStore(t)
	svc := newTestAuthService(t, store)
	user := newTestUser(t, store)
	newTestAPIToken(t, store, user.ID, "secret")

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
	s := newTestHandler(store, &ci.Runner{Store: store, Workspace: workspace.New(ws, filepath.Join(ws, "logs"))}, nil, ws, maxSnapshotSize, svc)
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
	req.Header.Set("Authorization", "Bearer secret")
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
	rr := serveAuthed(t, s, req)
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
	rr := serveAuthed(t, s, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var created struct {
		ID            int64 `json:"id"`
		Attempt       int   `json:"attempt"`
		AttemptsCount int   `json:"attempts_count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != previous.ID || created.Attempt != 2 || created.AttemptsCount != 2 {
		t.Fatalf("retry response should describe the execution's latest attempt: %+v", created)
	}

	execs, err := store.ListExecutions(t.Context(), projectID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(execs) != 1 {
		t.Fatalf("retry should not add an execution: %+v", execs)
	}
	if execs[0].ExecutionID != previous.ID || execs[0].Attempt != 2 || execs[0].AttemptsCount != 2 {
		t.Fatalf("retry not grouped: %+v", execs[0])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID.String()+"/executions/"+itoa(previous.ID)+"?attempt=1", nil)
	rr = serveAuthed(t, s, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for attempt 1, got %d: %s", rr.Code, rr.Body.String())
	}
	var first struct {
		ID      int64  `json:"id"`
		Attempt int    `json:"attempt"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.ID != previous.ID || first.Attempt != 1 || first.Status != string(stores.StatusFailed) {
		t.Fatalf("unexpected attempt 1: %+v", first)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID.String()+"/executions/"+itoa(previous.ID)+"/attempts", nil)
	rr = serveAuthed(t, s, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var attempts []stores.Attempt
	if err := json.Unmarshal(rr.Body.Bytes(), &attempts); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0].Number != 1 || attempts[1].Number != 2 || attempts[1].Trigger != stores.TriggerRetry {
		t.Fatalf("unexpected attempts: %+v", attempts)
	}

	// Retrying an older attempt would fork the execution and is rejected.
	req = httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID.String()+"/executions/"+itoa(previous.ID)+"/retry?attempt=1", nil)
	rr = serveAuthed(t, s, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for retrying an older attempt, got %d: %s", rr.Code, rr.Body.String())
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
	rr := serveAuthed(t, s, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}
