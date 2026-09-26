package stores

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/secrets"
)

type sqliteStore struct {
	db     *sql.DB
	cipher *secrets.Cipher
}

var _ Store = (*sqliteStore)(nil)

func (s *sqliteStore) Close() error { return s.db.Close() }

// SQLite has no native time, enum, or bool types: timestamps are stored as
// unix milliseconds (NULL when unset), enums are TEXT with a CHECK constraint,
// and booleans are INTEGER (0/1).

func (s *sqliteStore) CreateProject(ctx context.Context, p Project) (Project, error) {
	if p.Provider == "" {
		p.Provider = ProviderGeneric
	}
	if p.AuthType == "" {
		p.AuthType = AuthTypeNone
	}
	p.AuthSecret = encrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = encrypt(s.cipher, p.WebhookSecret)
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO projects (id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, p.ID.String(), p.Name, p.RepoURL, p.Provider, p.AuthType, p.AuthUser, p.AuthSecret, p.WebhookSecret, p.DefaultBranch, p.CreatedAt.UnixMilli(), p.UpdatedAt.UnixMilli())
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
		var id string
		var createdMs, updatedMs int64
		if err := rows.Scan(&id, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &createdMs, &updatedMs); err != nil {
			return nil, err
		}
		p.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
		p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
		p.CreatedAt = time.UnixMilli(createdMs)
		p.UpdatedAt = time.UnixMilli(updatedMs)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *sqliteStore) GetProject(ctx context.Context, id uuid.UUID) (Project, error) {
	var p Project
	var idStr string
	var createdMs, updatedMs int64
	err := s.db.QueryRowContext(ctx, `
        SELECT id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at
        FROM projects
        WHERE id = ?
    `, id.String()).Scan(&idStr, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &createdMs, &updatedMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	p.ID, err = uuid.Parse(idStr)
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	p.CreatedAt = time.UnixMilli(createdMs)
	p.UpdatedAt = time.UnixMilli(updatedMs)
	return p, nil
}

func (s *sqliteStore) GetProjectByName(ctx context.Context, name string) (Project, error) {
	var p Project
	var idStr string
	var createdMs, updatedMs int64
	err := s.db.QueryRowContext(ctx, `
        SELECT id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at
        FROM projects
        WHERE name = ?
    `, name).Scan(&idStr, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &createdMs, &updatedMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	p.ID, err = uuid.Parse(idStr)
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	p.CreatedAt = time.UnixMilli(createdMs)
	p.UpdatedAt = time.UnixMilli(updatedMs)
	return p, nil
}

func (s *sqliteStore) UpdateProject(ctx context.Context, p Project) (Project, error) {
	p.AuthSecret = encrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = encrypt(s.cipher, p.WebhookSecret)
	_, err := s.db.ExecContext(ctx, `
        UPDATE projects
        SET name = ?, repo_url = ?, provider = ?, auth_type = ?, auth_user = ?, auth_secret = ?, webhook_secret = ?, default_branch = ?, updated_at = ?
        WHERE id = ?
    `, p.Name, p.RepoURL, p.Provider, p.AuthType, p.AuthUser, p.AuthSecret, p.WebhookSecret, p.DefaultBranch, p.UpdatedAt.UnixMilli(), p.ID.String())
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	return p, nil
}

func (s *sqliteStore) DeleteProject(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id.String())
	return err
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if v.ProjectID == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE project_id IS NULL AND key = ?`, v.Key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO variables (project_id, key, value, secret) VALUES (NULL, ?, ?, ?)`, v.Key, value, secret); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE project_id = ? AND key = ?`, v.ProjectID.String(), v.Key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO variables (project_id, key, value, secret) VALUES (?, ?, ?, ?)`, v.ProjectID.String(), v.Key, value, secret); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) ListVariables(ctx context.Context, projectID *uuid.UUID) ([]Variable, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if projectID == nil {
		rows, err = s.db.QueryContext(ctx, `
            SELECT project_id, key, value, secret
            FROM variables
            WHERE project_id IS NULL
            ORDER BY key
        `)
	} else {
		rows, err = s.db.QueryContext(ctx, `
            SELECT project_id, key, value, secret
            FROM variables
            WHERE project_id = ?
            ORDER BY key
        `, projectID.String())
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	variables := make([]Variable, 0)
	for rows.Next() {
		var v Variable
		var pid *string
		var secret int
		if err := rows.Scan(&pid, &v.Key, &v.Value, &secret); err != nil {
			return nil, err
		}
		if pid != nil {
			u, err := uuid.Parse(*pid)
			if err != nil {
				return nil, err
			}
			v.ProjectID = &u
		}
		v.Secret = secret != 0
		if v.Secret {
			v.Value = decrypt(s.cipher, v.Value)
		}
		variables = append(variables, v)
	}
	return variables, rows.Err()
}

func (s *sqliteStore) GetVariable(ctx context.Context, projectID *uuid.UUID, key string) (Variable, error) {
	var v Variable
	var pid *string
	var secret int
	var err error
	if projectID == nil {
		err = s.db.QueryRowContext(ctx, `
            SELECT project_id, key, value, secret
            FROM variables
            WHERE project_id IS NULL AND key = ?
        `, key).Scan(&pid, &v.Key, &v.Value, &secret)
	} else {
		err = s.db.QueryRowContext(ctx, `
            SELECT project_id, key, value, secret
            FROM variables
            WHERE project_id = ? AND key = ?
        `, projectID.String(), key).Scan(&pid, &v.Key, &v.Value, &secret)
	}
	if err != nil {
		return Variable{}, err
	}
	if pid != nil {
		u, err := uuid.Parse(*pid)
		if err != nil {
			return Variable{}, err
		}
		v.ProjectID = &u
	}
	v.Secret = secret != 0
	if v.Secret {
		v.Value = decrypt(s.cipher, v.Value)
	}
	return v, nil
}

func (s *sqliteStore) DeleteVariable(ctx context.Context, projectID *uuid.UUID, key string) error {
	var err error
	if projectID == nil {
		_, err = s.db.ExecContext(ctx, `DELETE FROM variables WHERE project_id IS NULL AND key = ?`, key)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM variables WHERE project_id = ? AND key = ?`, projectID.String(), key)
	}
	return err
}

func (s *sqliteStore) CreateExecution(ctx context.Context, e Execution) error {
	if e.Status == "" {
		e.Status = StatusPending
	}
	if e.Trigger == "" {
		e.Trigger = TriggerManual
	}
	var startedMs, finishedMs any
	if e.StartedAt != nil {
		startedMs = e.StartedAt.UnixMilli()
	}
	if e.FinishedAt != nil {
		finishedMs = e.FinishedAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO executions (id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, e.ID.String(), e.ProjectID.String(), e.Workflow, e.Ref, e.CommitSHA, e.Status, e.Trigger, e.Steps, e.Error, startedMs, finishedMs, e.CreatedAt.UnixMilli(), e.ConcurrencyGroup)
	return err
}

func (s *sqliteStore) UpdateExecution(ctx context.Context, e Execution) error {
	var startedMs, finishedMs any
	if e.StartedAt != nil {
		startedMs = e.StartedAt.UnixMilli()
	}
	if e.FinishedAt != nil {
		finishedMs = e.FinishedAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, steps_json = ?, error = ?, started_at = ?, finished_at = ?, commit_sha = ?, concurrency_group = ?
        WHERE id = ?
    `, e.Status, e.Steps, e.Error, startedMs, finishedMs, e.CommitSHA, e.ConcurrencyGroup, e.ID.String())
	return err
}

func (s *sqliteStore) GetExecution(ctx context.Context, id uuid.UUID) (Execution, error) {
	var e Execution
	var idStr, projectIDStr string
	var startedMs, finishedMs *int64
	var createdMs int64
	err := s.db.QueryRowContext(ctx, `
        SELECT id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group
        FROM executions
        WHERE id = ?
    `, id.String()).Scan(&idStr, &projectIDStr, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &startedMs, &finishedMs, &createdMs, &e.ConcurrencyGroup)
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	if err != nil {
		return Execution{}, err
	}
	e.ID, err = uuid.Parse(idStr)
	if err != nil {
		return Execution{}, err
	}
	e.ProjectID, err = uuid.Parse(projectIDStr)
	if err != nil {
		return Execution{}, err
	}
	if startedMs != nil {
		t := time.UnixMilli(*startedMs)
		e.StartedAt = &t
	}
	if finishedMs != nil {
		t := time.UnixMilli(*finishedMs)
		e.FinishedAt = &t
	}
	e.CreatedAt = time.UnixMilli(createdMs)
	return e, nil
}

func (s *sqliteStore) ListExecutions(ctx context.Context, projectID uuid.UUID, limit int) ([]Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group
        FROM executions
        WHERE project_id = ?
        ORDER BY created_at DESC
        LIMIT ?
    `, projectID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	executions := make([]Execution, 0)
	for rows.Next() {
		var e Execution
		var idStr, projectIDStr string
		var startedMs, finishedMs *int64
		var createdMs int64
		if err := rows.Scan(&idStr, &projectIDStr, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &startedMs, &finishedMs, &createdMs, &e.ConcurrencyGroup); err != nil {
			return nil, err
		}
		e.ID, err = uuid.Parse(idStr)
		if err != nil {
			return nil, err
		}
		e.ProjectID, err = uuid.Parse(projectIDStr)
		if err != nil {
			return nil, err
		}
		if startedMs != nil {
			t := time.UnixMilli(*startedMs)
			e.StartedAt = &t
		}
		if finishedMs != nil {
			t := time.UnixMilli(*finishedMs)
			e.FinishedAt = &t
		}
		e.CreatedAt = time.UnixMilli(createdMs)
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

	parsed, err := uuid.Parse(id)
	if err != nil {
		return Execution{}, err
	}
	return s.GetExecution(ctx, parsed)
}

func (s *sqliteStore) RequeueOrphanedExecutions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, claimed_by = ?
        WHERE status = ?
    `, StatusPending, "", StatusRunning)
	return err
}

func (s *sqliteStore) SetCancelRequested(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE executions SET cancel_requested = 1 WHERE id = ?`, id.String())
	return err
}

func (s *sqliteStore) IsCancelRequested(ctx context.Context, id uuid.UUID) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT cancel_requested FROM executions WHERE id = ?`, id.String()).Scan(&n); errors.Is(err, sql.ErrNoRows) {
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

func (s *sqliteStore) CancelRunningInGroup(ctx context.Context, projectID uuid.UUID, group string, excludeID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET cancel_requested = 1
        WHERE project_id = ? AND concurrency_group = ? AND status = ? AND id != ?
    `, projectID.String(), group, StatusRunning, excludeID.String())
	return err
}

func (s *sqliteStore) UpsertSchedule(ctx context.Context, sch Schedule) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO schedules (project_id, workflow, cron_expr, next_run_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT (project_id, workflow) DO UPDATE SET cron_expr = excluded.cron_expr, next_run_at = excluded.next_run_at
    `, sch.ProjectID.String(), sch.Workflow, sch.CronExpr, sch.NextRunAt.UnixMilli())
	return err
}

func (s *sqliteStore) DeleteSchedule(ctx context.Context, projectID uuid.UUID, workflow string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE project_id = ? AND workflow = ?`, projectID.String(), workflow)
	return err
}

func (s *sqliteStore) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project_id, workflow, cron_expr, next_run_at FROM schedules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedules := make([]Schedule, 0)
	for rows.Next() {
		var sch Schedule
		var projectIDStr string
		var nextMs int64
		if err := rows.Scan(&projectIDStr, &sch.Workflow, &sch.CronExpr, &nextMs); err != nil {
			return nil, err
		}
		sch.ProjectID, err = uuid.Parse(projectIDStr)
		if err != nil {
			return nil, err
		}
		sch.NextRunAt = time.UnixMilli(nextMs)
		schedules = append(schedules, sch)
	}
	return schedules, rows.Err()
}

func (s *sqliteStore) ListDueSchedules(ctx context.Context, now time.Time) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT project_id, workflow, cron_expr, next_run_at
        FROM schedules
        WHERE next_run_at <= ?
    `, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedules := make([]Schedule, 0)
	for rows.Next() {
		var sch Schedule
		var projectIDStr string
		var nextMs int64
		if err := rows.Scan(&projectIDStr, &sch.Workflow, &sch.CronExpr, &nextMs); err != nil {
			return nil, err
		}
		sch.ProjectID, err = uuid.Parse(projectIDStr)
		if err != nil {
			return nil, err
		}
		sch.NextRunAt = time.UnixMilli(nextMs)
		schedules = append(schedules, sch)
	}
	return schedules, rows.Err()
}
