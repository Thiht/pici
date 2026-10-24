package variables

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
)

func newTestModule(t *testing.T) (*Module, stores.Store) {
	t.Helper()
	store, err := stores.Open("sqlite", filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store), store
}

func newTestProject(t *testing.T, store stores.Store) uuid.UUID {
	t.Helper()
	project := stores.Project{ID: uuid.New(), Name: "demo", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	created, err := store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func TestSecretsNeverLeave(t *testing.T) {
	module, store := newTestModule(t)
	projectID := newTestProject(t, store)
	ctx := context.Background()

	for _, variable := range []stores.Variable{
		{Key: "PLAIN", Value: "visible"},
		{Key: "TOKEN", Value: "s3cret", Secret: true},
	} {
		variable.ProjectID = &projectID
		saved, err := module.Set(ctx, variable)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Secret && saved.Value != "" {
			t.Fatalf("Set returned the secret value %q", saved.Value)
		}
	}

	variables, err := module.List(ctx, &projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(variables) != 2 {
		t.Fatalf("expected two variables, got %+v", variables)
	}
	for _, variable := range variables {
		if variable.Key == "TOKEN" && variable.Value != "" {
			t.Fatalf("List leaked the secret value %q", variable.Value)
		}
		if variable.Key == "PLAIN" && variable.Value != "visible" {
			t.Fatalf("List masked a plain value: %+v", variable)
		}
	}
}

func TestSetRequiresKey(t *testing.T) {
	module, _ := newTestModule(t)
	if _, err := module.Set(context.Background(), stores.Variable{Key: "  "}); !errors.Is(err, app.ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	module, store := newTestModule(t)
	projectID := newTestProject(t, store)
	ctx := context.Background()

	if _, err := module.Set(ctx, stores.Variable{ProjectID: &projectID, Key: "TOKEN", Secret: true}); err != nil {
		t.Fatal(err)
	}
	if err := module.Delete(ctx, &projectID, "TOKEN"); err != nil {
		t.Fatal(err)
	}
	variables, err := module.List(ctx, &projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(variables) != 0 {
		t.Fatalf("expected no variables left, got %+v", variables)
	}
}
