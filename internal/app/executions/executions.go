package executions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"uuid"

	"github.com/Thiht/pici/internal/app"
	"github.com/Thiht/pici/internal/ci"
	"github.com/Thiht/pici/internal/stores"
	"github.com/Thiht/pici/internal/workspace"
)

type Store interface {
	GetExecutionAttempt(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (stores.Execution, error)
	ListAttempts(ctx context.Context, projectID uuid.UUID, executionID int64) ([]stores.Execution, error)
	SetCancelRequested(ctx context.Context, projectID uuid.UUID, id int64) error
}

type Logs interface {
	ReadLogs(projectID uuid.UUID, execID int64) io.ReadCloser
	ReadSetupLog(projectID uuid.UUID, execID int64) (io.ReadCloser, error)
	ReadStepLog(projectID uuid.UUID, execID int64, index int, step string) (io.ReadCloser, error)
}

type Runner interface {
	Enqueue(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, trigger stores.Trigger, createdBy *uuid.UUID) (stores.Execution, error)
	EnqueueSnapshot(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, snapshotID uuid.UUID, createdBy *uuid.UUID) (stores.Execution, error)
	EnqueueRebuild(ctx context.Context, project stores.Project, previous stores.Execution, createdBy *uuid.UUID) (stores.Execution, error)
	EnqueueRetry(ctx context.Context, project stores.Project, previous stores.Execution, createdBy *uuid.UUID) (stores.Execution, error)
	Cancel(projectID uuid.UUID, execID int64)
}

type View struct {
	Execution stores.Execution
	Attempts  []stores.Execution
	Latest    bool
}

type Module struct {
	store     Store
	logs      Logs
	workspace *workspace.Workspace
	runner    Runner
}

func New(store Store, logs Logs, workspace *workspace.Workspace, runner Runner) *Module {
	return &Module{store: store, logs: logs, workspace: workspace, runner: runner}
}

// Get returns the requested Attempt, or the latest one when attempt is 0.
func (m *Module) Get(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (View, error) {
	execution, err := m.attempt(ctx, projectID, executionID, attempt)
	if err != nil {
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

func (m *Module) Create(ctx context.Context, project stores.Project, workflow, ref string, createdBy *uuid.UUID) (stores.Execution, error) {
	workflow = strings.TrimSpace(workflow)
	if workflow == "" {
		return stores.Execution{}, fmt.Errorf("%w: workflow is required", app.ErrInvalid)
	}
	if ref = strings.TrimSpace(ref); ref == "" {
		ref = project.DefaultBranch
	}
	return m.runner.Enqueue(ctx, project, workflow, ref, "", stores.TriggerManual, createdBy)
}

// CreateSnapshot stores an uploaded archive and queues a manual run whose
// source is that archive instead of a clone.
func (m *Module) CreateSnapshot(ctx context.Context, project stores.Project, workflow, ref, commitSHA string, archive io.Reader, createdBy *uuid.UUID) (stores.Execution, error) {
	workflow = strings.TrimSpace(workflow)
	if workflow == "" {
		return stores.Execution{}, fmt.Errorf("%w: workflow is required", app.ErrInvalid)
	}
	if ref = strings.TrimSpace(ref); ref == "" {
		ref = project.DefaultBranch
	}

	snapshotID := uuid.New()
	path := m.workspace.Snapshot(project.ID, snapshotID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return stores.Execution{}, err
	}
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return stores.Execution{}, err
	}
	if _, err := io.Copy(out, archive); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return stores.Execution{}, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return stores.Execution{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return stores.Execution{}, err
	}

	execution, err := m.runner.EnqueueSnapshot(ctx, project, workflow, ref, commitSHA, snapshotID, createdBy)
	if err != nil {
		_ = os.Remove(path)
		return stores.Execution{}, err
	}
	return execution, nil
}

func (m *Module) Cancel(ctx context.Context, projectID uuid.UUID, executionID int64) error {
	execution, err := m.attempt(ctx, projectID, executionID, 0)
	if err != nil {
		return err
	}
	if err := m.store.SetCancelRequested(ctx, projectID, execution.ID); err != nil {
		return err
	}
	m.runner.Cancel(projectID, execution.ID)
	return nil
}

func (m *Module) Rebuild(ctx context.Context, project stores.Project, executionID int64, createdBy *uuid.UUID) (stores.Execution, error) {
	previous, err := m.attempt(ctx, project.ID, executionID, 0)
	if err != nil {
		return stores.Execution{}, err
	}
	if previous.Source == stores.SourceSnapshot {
		return stores.Execution{}, fmt.Errorf("%w: snapshot executions cannot be rebuilt", app.ErrConflict)
	}
	return m.runner.EnqueueRebuild(ctx, project, previous, createdBy)
}

func (m *Module) Retry(ctx context.Context, project stores.Project, executionID int64, attempt int, createdBy *uuid.UUID) (stores.Execution, error) {
	previous, err := m.attempt(ctx, project.ID, executionID, attempt)
	if err != nil {
		return stores.Execution{}, err
	}
	if _, err := m.runner.EnqueueRetry(ctx, project, previous, createdBy); err != nil {
		switch {
		case errors.Is(err, ci.ErrWorkspaceGone):
			return stores.Execution{}, fmt.Errorf("%w: %s", app.ErrWorkspaceGone, err)
		case errors.Is(err, ci.ErrNotRetryable):
			return stores.Execution{}, fmt.Errorf("%w: %s", app.ErrNotRetryable, err)
		}
		return stores.Execution{}, err
	}
	view, err := m.Get(ctx, project.ID, executionID, 0)
	if err != nil {
		return stores.Execution{}, err
	}
	return view.Execution, nil
}

func (m *Module) Logs(execution stores.Execution) io.ReadCloser {
	return m.logs.ReadLogs(execution.ProjectID, execution.ID)
}

func (m *Module) SetupLog(execution stores.Execution) (io.ReadCloser, error) {
	logs, err := m.logs.ReadSetupLog(execution.ProjectID, execution.ID)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, app.ErrNotFound
	}
	return logs, err
}

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

func (m *Module) attempt(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (stores.Execution, error) {
	execution, err := m.store.GetExecutionAttempt(ctx, projectID, executionID, attempt)
	if errors.Is(err, stores.ErrNotFound) {
		return stores.Execution{}, app.ErrNotFound
	}
	return execution, err
}
