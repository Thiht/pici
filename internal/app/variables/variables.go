package variables

import (
	"context"
	"fmt"
	"strings"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
)

type Store interface {
	SetVariable(ctx context.Context, v stores.Variable) error
	ListVariables(ctx context.Context, projectID *uuid.UUID) ([]stores.Variable, error)
	DeleteVariable(ctx context.Context, projectID *uuid.UUID, key string) error
}

type Module struct {
	store Store
}

func New(store Store) *Module {
	return &Module{store: store}
}

// List returns the Variables of a Project, or the global ones when projectID
// is nil. Secret values are blank.
func (m *Module) List(ctx context.Context, projectID *uuid.UUID) ([]stores.Variable, error) {
	variables, err := m.store.ListVariables(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]stores.Variable, len(variables))
	for i, v := range variables {
		if v.Secret {
			v.Value = ""
		}
		out[i] = v
	}
	return out, nil
}

func (m *Module) Set(ctx context.Context, variable stores.Variable) (stores.Variable, error) {
	variable.Key = strings.TrimSpace(variable.Key)
	if variable.Key == "" {
		return stores.Variable{}, fmt.Errorf("%w: key is required", app.ErrInvalid)
	}
	if err := m.store.SetVariable(ctx, variable); err != nil {
		return stores.Variable{}, err
	}
	if variable.Secret {
		variable.Value = ""
	}
	return variable, nil
}

func (m *Module) Delete(ctx context.Context, projectID *uuid.UUID, key string) error {
	return m.store.DeleteVariable(ctx, projectID, key)
}
