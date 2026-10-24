package projects

import (
	"context"
	"errors"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
)

// Store is the part of the store this module needs.
type Store interface {
	GetProject(ctx context.Context, id uuid.UUID) (stores.Project, error)
	GetProjectByName(ctx context.Context, name string) (stores.Project, error)
}

// Module answers questions about Projects.
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
