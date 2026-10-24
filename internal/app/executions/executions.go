// Package executions serves the read side of an Execution: the selected
// Attempt, its siblings, and the logs of its setup phase and Steps.
package executions

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/stores"
)

// Store is the part of the store this module needs.
type Store interface {
	GetExecutionAttempt(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (stores.Execution, error)
	ListAttempts(ctx context.Context, projectID uuid.UUID, executionID int64) ([]stores.Execution, error)
}

// Logs opens an Execution's logs. *workspace.Workspace satisfies it.
type Logs interface {
	ReadLogs(projectID uuid.UUID, execID int64) io.ReadCloser
	ReadSetupLog(projectID uuid.UUID, execID int64) (io.ReadCloser, error)
	ReadStepLog(projectID uuid.UUID, execID int64, index int, step string) (io.ReadCloser, error)
}

// View is one Attempt of an Execution with the Attempts around it. It is what
// both the Execution page and the JSON surface render.
type View struct {
	Execution stores.Execution
	Attempts  []stores.Execution
	Latest    bool
}

// Module reads Executions.
type Module struct {
	store Store
	logs  Logs
}

func New(store Store, logs Logs) *Module {
	return &Module{store: store, logs: logs}
}

// Get returns the requested Attempt, or the latest one when attempt is 0.
func (m *Module) Get(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (View, error) {
	execution, err := m.store.GetExecutionAttempt(ctx, projectID, executionID, attempt)
	if err != nil {
		if errors.Is(err, stores.ErrNotFound) {
			return View{}, app.ErrNotFound
		}
		return View{}, err
	}
	attempts, err := m.store.ListAttempts(ctx, projectID, execution.ExecutionID)
	if err != nil {
		return View{}, err
	}
	return View{
		Execution: execution,
		Attempts:  attempts,
		Latest:    execution.Attempt == execution.AttemptsCount,
	}, nil
}

// Logs opens the whole log of an Attempt, setup included, as one stream.
func (m *Module) Logs(execution stores.Execution) io.ReadCloser {
	return m.logs.ReadLogs(execution.ProjectID, execution.ID)
}

// SetupLog opens the setup log of an Attempt.
func (m *Module) SetupLog(execution stores.Execution) (io.ReadCloser, error) {
	logs, err := m.logs.ReadSetupLog(execution.ProjectID, execution.ID)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, app.ErrNotFound
	}
	return logs, err
}

// StepLog opens the log of one Step, by name.
func (m *Module) StepLog(execution stores.Execution, step string) (io.ReadCloser, error) {
	index := -1
	for i, s := range execution.Steps {
		if s.Name == step {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, app.ErrNotFound
	}
	logs, err := m.logs.ReadStepLog(execution.ProjectID, execution.ID, index, step)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, app.ErrNotFound
	}
	return logs, err
}
