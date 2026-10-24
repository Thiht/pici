package projects

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

func TestResolveByIdAndName(t *testing.T) {
	module, store := newTestModule(t)
	project := stores.Project{
		ID: uuid.New(), Name: "demo", RepoURL: "https://example.com/acme/demo.git",
		DefaultBranch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	created, err := store.CreateProject(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{created.ID.String(), "demo"} {
		got, err := module.Resolve(context.Background(), ref)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", ref, err)
		}
		if got.ID != created.ID {
			t.Fatalf("Resolve(%q) = %v, want %v", ref, got.ID, created.ID)
		}
	}
}

func TestResolveNotFound(t *testing.T) {
	module, _ := newTestModule(t)

	for _, ref := range []string{"missing", uuid.New().String()} {
		if _, err := module.Resolve(context.Background(), ref); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("Resolve(%q) = %v, want ErrNotFound", ref, err)
		}
	}
}

func TestCreateDerivesIdentity(t *testing.T) {
	module, store := newTestModule(t)
	created, err := module.Create(context.Background(), stores.Project{
		Name: "demo", RepoURL: "https://github.com/acme/demo.git", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != stores.ProviderGithub || created.AuthType != stores.AuthTypeNone || created.AuthUser != "" {
		t.Fatalf("unexpected identity: %+v", created)
	}
	if created.ID == uuid.Nil() || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected an id and timestamps: %+v", created)
	}
	got, err := store.GetProject(context.Background(), created.ID)
	if err != nil || got.Name != "demo" {
		t.Fatalf("project was not stored: %+v %v", got, err)
	}
}

func TestCreateInfersTokenFromSecret(t *testing.T) {
	module, _ := newTestModule(t)
	created, err := module.Create(context.Background(), stores.Project{
		Name: "demo", RepoURL: "https://git.example.com/acme/demo.git",
		AuthSecret: "s3cret", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthType != stores.AuthTypeToken || created.AuthUser != "oauth2" {
		t.Fatalf("unexpected auth: %+v", created)
	}
}

func TestCreateProviderFromHost(t *testing.T) {
	cases := map[string]stores.Provider{
		"https://github.com/acme/demo.git":      stores.ProviderGithub,
		"git@github.com:acme/demo.git":          stores.ProviderGithub,
		"https://gitlab.com/group/demo.git":     stores.ProviderGitlab,
		"https://gitlab.example.com/group/demo": stores.ProviderGitlab,
		"https://github.com.example.com/acme/x": stores.ProviderGeneric,
		"https://git.example.com/acme/demo.git": stores.ProviderGeneric,
		"ssh://git@example.com/acme/demo.git":   stores.ProviderGeneric,
	}
	for repoURL, want := range cases {
		t.Run(repoURL, func(t *testing.T) {
			module, _ := newTestModule(t)
			created, err := module.Create(context.Background(), stores.Project{
				Name: "demo", RepoURL: repoURL, DefaultBranch: "main",
			})
			if err != nil {
				t.Fatal(err)
			}
			if created.Provider != want {
				t.Fatalf("provider = %q, want %q", created.Provider, want)
			}
		})
	}
}

func TestCreateValidates(t *testing.T) {
	module, _ := newTestModule(t)
	cases := map[string]stores.Project{
		"Name is required.":           {RepoURL: "https://github.com/acme/demo.git"},
		"Repository URL is required.": {Name: "demo"},
	}
	for message, draft := range cases {
		_, err := module.Create(context.Background(), draft)
		if !errors.Is(err, app.ErrInvalid) || err.Error() != message {
			t.Fatalf("Create() = %v, want ErrInvalid %q", err, message)
		}
	}
}

func TestUpdateNormalizes(t *testing.T) {
	module, _ := newTestModule(t)
	project, err := module.Create(context.Background(), stores.Project{
		Name: "demo", RepoURL: "https://github.com/acme/demo.git", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}

	project.Provider = ""
	project.AuthType = ""
	project.AuthSecret = "s3cret"
	updated, err := module.Update(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Provider != stores.ProviderGithub || updated.AuthType != stores.AuthTypeToken || updated.AuthUser != "oauth2" {
		t.Fatalf("unexpected identity after update: %+v", updated)
	}
	if !updated.UpdatedAt.After(project.UpdatedAt) && updated.UpdatedAt.Equal(project.UpdatedAt) {
		t.Fatalf("UpdatedAt was not refreshed: %+v", updated.UpdatedAt)
	}
}

func TestDeleteAndList(t *testing.T) {
	module, _ := newTestModule(t)
	ctx := context.Background()
	created, err := module.Create(ctx, stores.Project{Name: "demo", RepoURL: "https://github.com/acme/demo.git", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := module.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %d, %v", len(list), err)
	}
	if err := module.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Resolve(ctx, created.ID.String()); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDetectIsLenient(t *testing.T) {
	module, _ := newTestModule(t)
	detected := module.Detect(context.Background(), stores.Project{RepoURL: "https://gitlab.com/group/demo.git", DefaultBranch: "main"})
	if detected.Provider != stores.ProviderGitlab {
		t.Fatalf("provider = %q", detected.Provider)
	}
	if detected.AuthType != stores.AuthTypeNone || detected.AuthUser != "" {
		t.Fatalf("unexpected auth: %+v", detected)
	}
}

func TestDefaultAuthUser(t *testing.T) {
	cases := map[stores.AuthType]string{
		stores.AuthTypeToken: "oauth2",
		stores.AuthTypeSsh:   "git",
		stores.AuthTypeNone:  "",
	}
	for authType, want := range cases {
		if got := defaultAuthUser(authType); got != want {
			t.Errorf("defaultAuthUser(%q) = %q, want %q", authType, got, want)
		}
	}
}

func TestDetectSwapsPlaceholderAuthUser(t *testing.T) {
	module, _ := newTestModule(t)
	for authType, want := range map[stores.AuthType]string{
		stores.AuthTypeSsh:   "git",
		stores.AuthTypeToken: "oauth2",
		stores.AuthTypeNone:  "",
	} {
		detected := module.Detect(context.Background(), stores.Project{
			RepoURL: "https://example.com/acme/demo.git", DefaultBranch: "main",
			AuthType: authType, AuthUser: "oauth2",
		})
		if detected.AuthUser != want {
			t.Errorf("AuthUser for %q = %q, want %q", authType, detected.AuthUser, want)
		}
	}
}
