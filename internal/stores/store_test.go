package stores

import (
	"context"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/Thiht/pici/internal/secrets"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestProjectCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := Project{
		ID:         "p1",
		Name:       "demo",
		RepoURL:    "https://github.com/foo/bar.git",
		Provider:   ProviderGitHub,
		AuthType:   AuthTypeToken,
		AuthSecret: "secret-token",
		CreatedAt:  1,
		UpdatedAt:  1,
	}
	if _, err := s.CreateProject(ctx, p); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetProject(ctx, "p1")
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

	if err := s.DeleteProject(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetProject(ctx, "p1"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestVariableCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetVariable(ctx, Variable{ProjectID: "p1", Key: "FOO", Value: "bar"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVariable(ctx, Variable{ProjectID: "", Key: "GLOBAL", Value: "x", Secret: true}); err != nil {
		t.Fatal(err)
	}

	projectVars, err := s.ListVariables(ctx, "p1")
	if err != nil || len(projectVars) != 1 {
		t.Fatalf("expected 1 project var, got %d (err=%v)", len(projectVars), err)
	}
	globalVars, err := s.ListVariables(ctx, "")
	if err != nil || len(globalVars) != 1 || !globalVars[0].Secret {
		t.Fatalf("unexpected global vars: %+v (err=%v)", globalVars, err)
	}

	v, err := s.GetVariable(ctx, "p1", "FOO")
	if err != nil || v.Value != "bar" {
		t.Fatalf("unexpected variable: %+v (err=%v)", v, err)
	}

	if err := s.DeleteVariable(ctx, "p1", "FOO"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetVariable(ctx, "p1", "FOO"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestExecutionCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	e := Execution{
		ID:        "e1",
		ProjectID: "p1",
		Workflow:  "build",
		Status:    StatusPending,
		Steps:     []StepResult{{Name: "install", Status: StepStatusPending}},
	}
	if err := s.CreateExecution(ctx, e); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetExecution(ctx, "e1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 1 || got.Steps[0].Name != "install" {
		t.Fatalf("unexpected steps: %+v", got.Steps)
	}

	got.Status = StatusSuccess
	if err := s.UpdateExecution(ctx, got); err != nil {
		t.Fatal(err)
	}

	updated, _ := s.GetExecution(ctx, "e1")
	if updated.Status != StatusSuccess {
		t.Fatalf("expected success, got %s", updated.Status)
	}

	list, err := s.ListExecutions(ctx, "p1", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 execution, got %d (err=%v)", len(list), err)
	}
}

func TestCancelRunningInGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, e := range []Execution{
		{ID: "e1", ProjectID: "p1", Workflow: "deploy", Status: StatusRunning, ConcurrencyGroup: "deploy"},
		{ID: "e2", ProjectID: "p1", Workflow: "deploy", Status: StatusRunning, ConcurrencyGroup: "deploy"},
		{ID: "e3", ProjectID: "p1", Workflow: "deploy", Status: StatusRunning, ConcurrencyGroup: "other"},
	} {
		if err := s.CreateExecution(ctx, e); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.CancelRunningInGroup(ctx, "p1", "deploy", "e2"); err != nil {
		t.Fatal(err)
	}

	if ok, _ := s.IsCancelRequested(ctx, "e1"); !ok {
		t.Fatal("expected e1 to be canceled")
	}
	if ok, _ := s.IsCancelRequested(ctx, "e2"); ok {
		t.Fatal("e2 should not be canceled (excluded)")
	}
	if ok, _ := s.IsCancelRequested(ctx, "e3"); ok {
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

	if err := s.SetVariable(ctx, Variable{ProjectID: "p1", Key: "TOKEN", Value: "hunter2", Secret: true}); err != nil {
		t.Fatal(err)
	}

	vars, err := s.ListVariables(ctx, "p1")
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
	if err := raw.QueryRowContext(ctx, `SELECT value FROM variables WHERE project_id = 'p1' AND key = 'TOKEN'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "hunter2" || stored == "" {
		t.Fatalf("secret stored in plaintext: %q", stored)
	}
}
