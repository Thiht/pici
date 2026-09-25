package stores

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Thiht/pici/internal/secrets"
)

type sqliteStore struct {
	db     *sql.DB
	cipher *secrets.Cipher
}

var _ Store = (*sqliteStore)(nil)

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) CreateProject(ctx context.Context, p Project) (Project, error) {
	p.AuthSecret = encrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = encrypt(s.cipher, p.WebhookSecret)
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO projects (id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, p.ID, p.Name, p.RepoURL, p.Provider, p.AuthType, p.AuthUser, p.AuthSecret, p.WebhookSecret, p.DefaultBranch, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	return p, nil
}

func (s *sqliteStore) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at
        FROM projects
        ORDER BY name
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
		p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *sqliteStore) GetProject(ctx context.Context, id string) (Project, error) {
	var p Project
	err := s.db.QueryRowContext(ctx, `
        SELECT id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at
        FROM projects
        WHERE id = ?
    `, id).Scan(&p.ID, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	return p, nil
}

func (s *sqliteStore) GetProjectByName(ctx context.Context, name string) (Project, error) {
	var p Project
	err := s.db.QueryRowContext(ctx, `
        SELECT id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at
        FROM projects
        WHERE name = ?
    `, name).Scan(&p.ID, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	return p, nil
}

func (s *sqliteStore) UpdateProject(ctx context.Context, p Project) (Project, error) {
	p.AuthSecret = encrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = encrypt(s.cipher, p.WebhookSecret)
	_, err := s.db.ExecContext(ctx, `
        UPDATE projects
        SET name = ?, repo_url = ?, provider = ?, auth_type = ?, auth_user = ?, auth_secret = ?, webhook_secret = ?, default_branch = ?, updated_at = ?
        WHERE id = ?
    `, p.Name, p.RepoURL, p.Provider, p.AuthType, p.AuthUser, p.AuthSecret, p.WebhookSecret, p.DefaultBranch, p.UpdatedAt, p.ID)
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	return p, nil
}

func (s *sqliteStore) DeleteProject(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM variables WHERE project_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE project_id = ?`, id); err != nil {
		return err
	}
	return nil
}

func (s *sqliteStore) SetVariable(ctx context.Context, v Variable) error {
	value := v.Value
	if v.Secret {
		value = encrypt(s.cipher, value)
	}
	secret := 0
	if v.Secret {
		secret = 1
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO variables (project_id, key, value, secret)
        VALUES (?, ?, ?, ?)
        ON CONFLICT (project_id, key) DO UPDATE SET value = excluded.value, secret = excluded.secret
    `, v.ProjectID, v.Key, value, secret)
	return err
}

func (s *sqliteStore) ListVariables(ctx context.Context, projectID string) ([]Variable, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT project_id, key, value, secret
        FROM variables
        WHERE project_id = ?
        ORDER BY key
    `, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	variables := make([]Variable, 0)
	for rows.Next() {
		var v Variable
		var secret int
		if err := rows.Scan(&v.ProjectID, &v.Key, &v.Value, &secret); err != nil {
			return nil, err
		}
		v.Secret = secret != 0
		if v.Secret {
			v.Value = decrypt(s.cipher, v.Value)
		}
		variables = append(variables, v)
	}
	return variables, rows.Err()
}

func (s *sqliteStore) GetVariable(ctx context.Context, projectID, key string) (Variable, error) {
	var v Variable
	var secret int
	if err := s.db.QueryRowContext(ctx, `
        SELECT project_id, key, value, secret
        FROM variables
        WHERE project_id = ? AND key = ?
    `, projectID, key).Scan(&v.ProjectID, &v.Key, &v.Value, &secret); err != nil {
		return Variable{}, err
	}
	v.Secret = secret != 0
	if v.Secret {
		v.Value = decrypt(s.cipher, v.Value)
	}
	return v, nil
}

func (s *sqliteStore) DeleteVariable(ctx context.Context, projectID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM variables WHERE project_id = ? AND key = ?`, projectID, key)
	return err
}

func (s *sqliteStore) CreateExecution(ctx context.Context, e Execution) error {
	stepsJSON, err := marshalSteps(e.Steps)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
        INSERT INTO executions (id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, e.ID, e.ProjectID, e.Workflow, e.Ref, e.CommitSHA, e.Status, e.Trigger, stepsJSON, e.Error, e.StartedAt, e.FinishedAt, e.CreatedAt, e.ConcurrencyGroup)
	return err
}

func (s *sqliteStore) UpdateExecution(ctx context.Context, e Execution) error {
	stepsJSON, err := marshalSteps(e.Steps)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, steps_json = ?, error = ?, started_at = ?, finished_at = ?, commit_sha = ?, concurrency_group = ?
        WHERE id = ?
    `, e.Status, stepsJSON, e.Error, e.StartedAt, e.FinishedAt, e.CommitSHA, e.ConcurrencyGroup, e.ID)
	return err
}

func (s *sqliteStore) GetExecution(ctx context.Context, id string) (Execution, error) {
	var e Execution
	var stepsJSON string
	err := s.db.QueryRowContext(ctx, `
        SELECT id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group
        FROM executions
        WHERE id = ?
    `, id).Scan(&e.ID, &e.ProjectID, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &stepsJSON, &e.Error, &e.StartedAt, &e.FinishedAt, &e.CreatedAt, &e.ConcurrencyGroup)
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	if err != nil {
		return Execution{}, err
	}
	e.Steps, _ = unmarshalSteps(stepsJSON)
	return e, nil
}

func (s *sqliteStore) ListExecutions(ctx context.Context, projectID string, limit int) ([]Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group
        FROM executions
        WHERE project_id = ?
        ORDER BY created_at DESC
        LIMIT ?
    `, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	executions := make([]Execution, 0)
	for rows.Next() {
		var e Execution
		var stepsJSON string
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &stepsJSON, &e.Error, &e.StartedAt, &e.FinishedAt, &e.CreatedAt, &e.ConcurrencyGroup); err != nil {
			return nil, err
		}
		e.Steps, _ = unmarshalSteps(stepsJSON)
		executions = append(executions, e)
	}
	return executions, rows.Err()
}

func (s *sqliteStore) ClaimPendingExecution(ctx context.Context, workerID string) (Execution, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Execution{}, err
	}
	defer tx.Rollback()

	var id string
	if err := tx.QueryRowContext(ctx, `
        SELECT id
        FROM executions
        WHERE status = ?
        ORDER BY created_at ASC
        LIMIT 1
    `, StatusPending).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	} else if err != nil {
		return Execution{}, err
	}

	result, err := tx.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, claimed_by = ?, started_at = ?
        WHERE id = ? AND status = ?
    `, StatusRunning, workerID, time.Now().UnixMilli(), id, StatusPending)
	if err != nil {
		return Execution{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return Execution{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return Execution{}, err
	}

	return s.GetExecution(ctx, id)
}

func (s *sqliteStore) RequeueOrphanedExecutions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, claimed_by = ?
        WHERE status = ?
    `, StatusPending, "", StatusRunning)
	return err
}

func (s *sqliteStore) SetCancelRequested(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE executions SET cancel_requested = 1 WHERE id = ?`, id)
	return err
}

func (s *sqliteStore) IsCancelRequested(ctx context.Context, id string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT cancel_requested FROM executions WHERE id = ?`, id).Scan(&n); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	return n != 0, nil
}

func (s *sqliteStore) CountPendingExecutions(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM executions WHERE status = ?`, StatusPending).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *sqliteStore) CancelRunningInGroup(ctx context.Context, projectID, group, excludeID string) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET cancel_requested = 1
        WHERE project_id = ? AND concurrency_group = ? AND status = ? AND id != ?
    `, projectID, group, StatusRunning, excludeID)
	return err
}

func (s *sqliteStore) UpsertSchedule(ctx context.Context, sch Schedule) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO schedules (project_id, workflow, cron_expr, next_run_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT (project_id, workflow) DO UPDATE SET cron_expr = excluded.cron_expr, next_run_at = excluded.next_run_at
    `, sch.ProjectID, sch.Workflow, sch.CronExpr, sch.NextRunAt)
	return err
}

func (s *sqliteStore) DeleteSchedule(ctx context.Context, projectID, workflow string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE project_id = ? AND workflow = ?`, projectID, workflow)
	return err
}

func (s *sqliteStore) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project_id, workflow, cron_expr, next_run_at FROM schedules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSchedules(rows)
}

func (s *sqliteStore) ListDueSchedules(ctx context.Context, now int64) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT project_id, workflow, cron_expr, next_run_at
        FROM schedules
        WHERE next_run_at <= ?
    `, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSchedules(rows)
}
