package stores

import (
	"context"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/secrets"
)

func mustUUID(s string) uuid.UUID {
	return uuid.MustParse(s)
}

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrations(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	id := mustUUID("11111111-1111-1111-1111-111111111111")

	first, err := Open("sqlite", path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := first.CreateProject(ctx, Project{ID: id, Name: "demo", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopening an existing database must not re-apply migrations nor lose data.
	second, err := Open("sqlite", path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	p, err := second.GetProject(ctx, id)
	if err != nil || p.Name != "demo" {
		t.Fatalf("unexpected project: %+v (err=%v)", p, err)
	}

	raw, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	var version int64
	if err := raw.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil {
		t.Fatalf("goose version table: %v", err)
	}
	if version != 4 {
		t.Fatalf("expected migration version 4, got %d", version)
	}
}

func TestStepsEnvRoundTrip(t *testing.T) {
	steps := Steps{{Name: "build", Status: StepStatusSuccess, Env: []string{"CI=true", "TOKEN=***"}}}
	v, err := steps.Value()
	if err != nil {
		t.Fatal(err)
	}
	var got Steps
	if err := got.Scan(v); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !slices.Equal(got[0].Env, []string{"CI=true", "TOKEN=***"}) {
		t.Fatalf("env not preserved: %+v", got)
	}
}

func TestProjectCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mustUUID("11111111-1111-1111-1111-111111111111")

	p := Project{
		ID:         id,
		Name:       "demo",
		RepoURL:    "https://github.com/foo/bar.git",
		Provider:   ProviderGithub,
		AuthType:   AuthTypeToken,
		AuthSecret: "secret-token",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if _, err := s.CreateProject(ctx, p); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetProject(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || got.AuthSecret != "secret-token" {
		t.Fatalf("unexpected project: %+v", got)
	}

	list, err := s.ListProjects(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 project, got %d (err=%v)", len(list), err)
	}

	if err := s.DeleteProject(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetProject(ctx, id); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestVariableCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := mustUUID("11111111-1111-1111-1111-111111111111")

	if _, err := s.CreateProject(ctx, Project{ID: id, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	if err := s.SetVariable(ctx, Variable{ProjectID: &id, Key: "FOO", Value: "bar"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVariable(ctx, Variable{ProjectID: nil, Key: "GLOBAL", Value: "x", Secret: true}); err != nil {
		t.Fatal(err)
	}

	projectVars, err := s.ListVariables(ctx, &id)
	if err != nil || len(projectVars) != 1 {
		t.Fatalf("expected 1 project var, got %d (err=%v)", len(projectVars), err)
	}
	globalVars, err := s.ListVariables(ctx, nil)
	if err != nil || len(globalVars) != 1 || !globalVars[0].Secret {
		t.Fatalf("unexpected global vars: %+v (err=%v)", globalVars, err)
	}

	v, err := s.GetVariable(ctx, &id, "FOO")
	if err != nil || v.Value != "bar" {
		t.Fatalf("unexpected variable: %+v (err=%v)", v, err)
	}

	if err := s.DeleteVariable(ctx, &id, "FOO"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetVariable(ctx, &id, "FOO"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestExecutionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	projectID := mustUUID("11111111-1111-1111-1111-111111111111")

	if _, err := s.CreateProject(ctx, Project{ID: projectID, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	e := Execution{
		ProjectID: projectID,
		Workflow:  "build",
		Status:    StatusPending,
		Steps:     []StepResult{{Name: "install", Status: StepStatusPending}},
	}
	if err := s.CreateExecution(ctx, &e); err != nil {
		t.Fatal(err)
	}
	if e.ID != 1 {
		t.Fatalf("expected first execution id 1, got %d", e.ID)
	}

	got, err := s.GetExecution(ctx, projectID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 1 || got.Steps[0].Name != "install" {
		t.Fatalf("unexpected steps: %+v", got.Steps)
	}

	got.Status = StatusSuccess
	started := time.Now().Add(-time.Minute)
	setupFinished := started.Add(10 * time.Second)
	got.StartedAt = &started
	got.SetupFinishedAt = &setupFinished
	if err := s.UpdateExecution(ctx, got); err != nil {
		t.Fatal(err)
	}

	updated, _ := s.GetExecution(ctx, projectID, e.ID)
	if updated.Status != StatusSuccess {
		t.Fatalf("expected success, got %s", updated.Status)
	}
	if updated.SetupFinishedAt == nil || updated.SetupFinishedAt.UnixMilli() != setupFinished.UnixMilli() {
		t.Fatalf("setup_finished_at not persisted: %+v", updated.SetupFinishedAt)
	}

	list, err := s.ListExecutions(ctx, projectID, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(list), err)
	}
	if list[0].SetupFinishedAt == nil || list[0].SetupFinishedAt.UnixMilli() != setupFinished.UnixMilli() {
		t.Fatalf("setup_finished_at not listed: %+v", list[0].SetupFinishedAt)
	}
}

func TestExecutionSourceRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	projectID := mustUUID("33333333-3333-3333-3333-333333333333")

	if _, err := s.CreateProject(ctx, Project{ID: projectID, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	snapshotID := mustUUID("44444444-4444-4444-4444-444444444444")
	e := Execution{
		ProjectID:  projectID,
		Workflow:   "build",
		Status:     StatusPending,
		Source:     SourceSnapshot,
		SnapshotID: &snapshotID,
	}
	if err := s.CreateExecution(ctx, &e); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetExecution(ctx, projectID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != SourceSnapshot {
		t.Fatalf("source = %q, want snapshot", got.Source)
	}
	if got.SnapshotID == nil || *got.SnapshotID != snapshotID {
		t.Fatalf("snapshot id = %v, want %s", got.SnapshotID, snapshotID)
	}

	list, err := s.ListExecutions(ctx, projectID, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(list), err)
	}
	if list[0].Source != SourceSnapshot {
		t.Fatalf("listed source = %q, want snapshot", list[0].Source)
	}
}

func TestCancelRunningInGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	projectID := mustUUID("11111111-1111-1111-1111-111111111111")

	if _, err := s.CreateProject(ctx, Project{ID: projectID, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	var ids []int64
	for _, e := range []Execution{
		{ProjectID: projectID, Workflow: "deploy", Status: StatusRunning, ConcurrencyGroup: "deploy"},
		{ProjectID: projectID, Workflow: "deploy", Status: StatusRunning, ConcurrencyGroup: "deploy"},
		{ProjectID: projectID, Workflow: "deploy", Status: StatusRunning, ConcurrencyGroup: "other"},
	} {
		if err := s.CreateExecution(ctx, &e); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}

	if err := s.CancelRunningInGroup(ctx, projectID, "deploy", ids[1]); err != nil {
		t.Fatal(err)
	}

	if ok, _ := s.IsCancelRequested(ctx, projectID, ids[0]); !ok {
		t.Fatal("expected e1 to be canceled")
	}
	if ok, _ := s.IsCancelRequested(ctx, projectID, ids[1]); ok {
		t.Fatal("e2 should not be canceled (excluded)")
	}
	if ok, _ := s.IsCancelRequested(ctx, projectID, ids[2]); ok {
		t.Fatal("e3 should not be canceled (different group)")
	}
}

func TestSecretEncryptionAtRest(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "enc.db")
	key := hex.EncodeToString(make([]byte, 32))
	cipher, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Open("sqlite", path, cipher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	id := mustUUID("11111111-1111-1111-1111-111111111111")
	if _, err := s.CreateProject(ctx, Project{ID: id, Name: "demo"}); err != nil {
		t.Fatal(err)
	}

	if err := s.SetVariable(ctx, Variable{ProjectID: &id, Key: "TOKEN", Value: "hunter2", Secret: true}); err != nil {
		t.Fatal(err)
	}

	vars, err := s.ListVariables(ctx, &id)
	if err != nil || len(vars) != 1 {
		t.Fatalf("list: %v (n=%d)", err, len(vars))
	}
	if vars[0].Value != "hunter2" {
		t.Fatalf("expected decrypted value, got %q", vars[0].Value)
	}

	raw, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var stored string
	if err := raw.QueryRowContext(ctx, `SELECT value FROM variables WHERE project_id = '11111111-1111-1111-1111-111111111111' AND key = 'TOKEN'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "hunter2" || stored == "" {
		t.Fatalf("secret stored in plaintext: %q", stored)
	}
}
