package projects

import (
	"context"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/git"
	"github.com/Thiht/pici/internal/github"
	"github.com/Thiht/pici/internal/stores"
)

type Store interface {
	CreateProject(ctx context.Context, p stores.Project) (stores.Project, error)
	ListProjects(ctx context.Context) ([]stores.Project, error)
	GetProject(ctx context.Context, id uuid.UUID) (stores.Project, error)
	GetProjectByName(ctx context.Context, name string) (stores.Project, error)
	UpdateProject(ctx context.Context, p stores.Project) (stores.Project, error)
	DeleteProject(ctx context.Context, id uuid.UUID) error
}

type Module struct {
	store Store
}

func New(store Store) *Module {
	return &Module{store: store}
}

// Resolve finds a Project by id or by name, the two ways a URL addresses one.
func (m *Module) Resolve(ctx context.Context, idOrName string) (stores.Project, error) {
	if id, err := uuid.Parse(idOrName); err == nil {
		project, err := m.store.GetProject(ctx, id)
		if err == nil {
			return project, nil
		}
		if !errors.Is(err, stores.ErrNotFound) {
			return stores.Project{}, err
		}
	}
	project, err := m.store.GetProjectByName(ctx, idOrName)
	if errors.Is(err, stores.ErrNotFound) {
		return stores.Project{}, app.ErrNotFound
	}
	return project, err
}

func (m *Module) List(ctx context.Context) ([]stores.Project, error) {
	return m.store.ListProjects(ctx)
}

func (m *Module) Create(ctx context.Context, draft stores.Project) (stores.Project, error) {
	project, err := m.normalize(ctx, draft)
	if err != nil {
		return stores.Project{}, err
	}
	project.ID = uuid.New()
	project.CreatedAt = time.Now()
	project.UpdatedAt = project.CreatedAt
	return m.store.CreateProject(ctx, project)
}

func (m *Module) Update(ctx context.Context, project stores.Project) (stores.Project, error) {
	normalized, err := m.normalize(ctx, project)
	if err != nil {
		return stores.Project{}, err
	}
	normalized.UpdatedAt = time.Now()
	return m.store.UpdateProject(ctx, normalized)
}

func (m *Module) Delete(ctx context.Context, id uuid.UUID) error {
	return m.store.DeleteProject(ctx, id)
}

// Detect fills the fields a draft allows us to derive, without storing
// anything. It is lenient: a partial draft yields a partial result, so a form
// can preview what it knows while the user types.
func (m *Module) Detect(ctx context.Context, draft stores.Project) stores.Project {
	draft.Name = strings.TrimSpace(draft.Name)
	draft.RepoURL = strings.TrimSpace(draft.RepoURL)
	draft.AuthUser = strings.TrimSpace(draft.AuthUser)
	draft.DefaultBranch = strings.TrimSpace(draft.DefaultBranch)
	if draft.RepoURL != "" {
		draft.Provider = providerFor(draft.RepoURL)
	}
	if !draft.AuthType.IsValid() {
		draft.AuthType = stores.AuthTypeNone
	}
	// A form preview swaps the placeholder user when the auth type changes;
	// "oauth2" and "git" are the only values we ever fill in ourselves.
	if draft.AuthUser == "" || draft.AuthUser == "oauth2" || draft.AuthUser == "git" {
		draft.AuthUser = defaultAuthUser(draft.AuthType)
	}
	if draft.DefaultBranch == "" && draft.RepoURL != "" {
		if refs, err := git.ListRefs(ctx, draft.RepoURL, Auth(draft)); err == nil {
			draft.DefaultBranch = refs.Head
		}
	}
	return draft
}

func Auth(project stores.Project) git.Auth {
	return git.Auth{Type: project.AuthType.String(), User: project.AuthUser, Secret: project.AuthSecret}
}

// normalize applies the defaults a Project can derive and rejects what it
// cannot: a Project always has a name, a URL, a valid provider and auth type,
// and a branch to run on.
func (m *Module) normalize(ctx context.Context, project stores.Project) (stores.Project, error) {
	project.Name = strings.TrimSpace(project.Name)
	project.RepoURL = strings.TrimSpace(project.RepoURL)
	project.AuthUser = strings.TrimSpace(project.AuthUser)
	project.DefaultBranch = strings.TrimSpace(project.DefaultBranch)
	if project.Name == "" {
		return project, app.Invalidf("Name is required.")
	}
	if project.RepoURL == "" {
		return project, app.Invalidf("Repository URL is required.")
	}
	if !project.Provider.IsValid() {
		project.Provider = providerFor(project.RepoURL)
	}
	if !project.AuthType.IsValid() {
		project.AuthType = stores.AuthTypeNone
		if project.AuthSecret != "" {
			project.AuthType = stores.AuthTypeToken
		}
	}
	if project.AuthUser == "" {
		project.AuthUser = defaultAuthUser(project.AuthType)
	}
	if project.DefaultBranch == "" {
		if refs, err := git.ListRefs(ctx, project.RepoURL, Auth(project)); err == nil {
			project.DefaultBranch = refs.Head
		}
	}
	return project, nil
}

// providerFor names the provider from the repository host. A self-hosted
// GitLab is still a GitLab; anything else is generic.
func providerFor(repoURL string) stores.Provider {
	host, _, _ := github.ParseRemote(repoURL)
	switch {
	case host == "github.com", host == "www.github.com":
		return stores.ProviderGithub
	case host == "gitlab.com", strings.Contains(host, "gitlab"):
		return stores.ProviderGitlab
	default:
		return stores.ProviderGeneric
	}
}

func defaultAuthUser(authType stores.AuthType) string {
	switch authType {
	case stores.AuthTypeToken:
		return "oauth2"
	case stores.AuthTypeSsh:
		return "git"
	default:
		return ""
	}
}
