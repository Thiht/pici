package stores

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/secrets"
)

type postgresStore struct {
	db     *sql.DB
	cipher *secrets.Cipher
}

var _ Store = (*postgresStore)(nil)

func (s *postgresStore) Close() error { return s.db.Close() }

// PostgreSQL uses native types: uuid ids, enum types, timestamptz, boolean,
// and jsonb. time.Time values map directly to timestamptz (NULL when nil).

func (s *postgresStore) CreateProject(ctx context.Context, p Project) (Project, error) {
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
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `, p.ID.String(), p.Name, p.RepoURL, p.Provider, p.AuthType, p.AuthUser, p.AuthSecret, p.WebhookSecret, p.DefaultBranch, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	return p, nil
}

func (s *postgresStore) ListProjects(ctx context.Context) ([]Project, error) {
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
		if err := rows.Scan(&id, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, err
		}
		p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
		p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *postgresStore) GetProject(ctx context.Context, id uuid.UUID) (Project, error) {
	var p Project
	var idStr string
	err := s.db.QueryRowContext(ctx, `
        SELECT id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at
        FROM projects
        WHERE id = $1
    `, id.String()).Scan(&idStr, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &p.CreatedAt, &p.UpdatedAt)
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
	return p, nil
}

func (s *postgresStore) GetProjectByName(ctx context.Context, name string) (Project, error) {
	var p Project
	var idStr string
	err := s.db.QueryRowContext(ctx, `
        SELECT id, name, repo_url, provider, auth_type, auth_user, auth_secret, webhook_secret, default_branch, created_at, updated_at
        FROM projects
        WHERE name = $1
    `, name).Scan(&idStr, &p.Name, &p.RepoURL, &p.Provider, &p.AuthType, &p.AuthUser, &p.AuthSecret, &p.WebhookSecret, &p.DefaultBranch, &p.CreatedAt, &p.UpdatedAt)
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
	return p, nil
}

func (s *postgresStore) UpdateProject(ctx context.Context, p Project) (Project, error) {
	p.AuthSecret = encrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = encrypt(s.cipher, p.WebhookSecret)
	_, err := s.db.ExecContext(ctx, `
        UPDATE projects
        SET name = $1, repo_url = $2, provider = $3, auth_type = $4, auth_user = $5, auth_secret = $6, webhook_secret = $7, default_branch = $8, updated_at = $9
        WHERE id = $10
    `, p.Name, p.RepoURL, p.Provider, p.AuthType, p.AuthUser, p.AuthSecret, p.WebhookSecret, p.DefaultBranch, p.UpdatedAt, p.ID.String())
	if err != nil {
		return Project{}, err
	}
	p.AuthSecret = decrypt(s.cipher, p.AuthSecret)
	p.WebhookSecret = decrypt(s.cipher, p.WebhookSecret)
	return p, nil
}

func (s *postgresStore) DeleteProject(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id.String())
	return err
}

func (s *postgresStore) SetVariable(ctx context.Context, v Variable) error {
	value := v.Value
	if v.Secret {
		value = encrypt(s.cipher, value)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if v.ProjectID == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE project_id IS NULL AND key = $1`, v.Key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO variables (project_id, key, value, secret) VALUES (NULL, $1, $2, $3)`, v.Key, value, v.Secret); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE project_id = $1 AND key = $2`, v.ProjectID.String(), v.Key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO variables (project_id, key, value, secret) VALUES ($1, $2, $3, $4)`, v.ProjectID.String(), v.Key, value, v.Secret); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *postgresStore) ListVariables(ctx context.Context, projectID *uuid.UUID) ([]Variable, error) {
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
            WHERE project_id = $1
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
		if err := rows.Scan(&pid, &v.Key, &v.Value, &v.Secret); err != nil {
			return nil, err
		}
		if pid != nil {
			u, err := uuid.Parse(*pid)
			if err != nil {
				return nil, err
			}
			v.ProjectID = &u
		}
		if v.Secret {
			v.Value = decrypt(s.cipher, v.Value)
		}
		variables = append(variables, v)
	}
	return variables, rows.Err()
}

func (s *postgresStore) GetVariable(ctx context.Context, projectID *uuid.UUID, key string) (Variable, error) {
	var v Variable
	var pid *string
	var err error
	if projectID == nil {
		err = s.db.QueryRowContext(ctx, `
            SELECT project_id, key, value, secret
            FROM variables
            WHERE project_id IS NULL AND key = $1
        `, key).Scan(&pid, &v.Key, &v.Value, &v.Secret)
	} else {
		err = s.db.QueryRowContext(ctx, `
            SELECT project_id, key, value, secret
            FROM variables
            WHERE project_id = $1 AND key = $2
        `, projectID.String(), key).Scan(&pid, &v.Key, &v.Value, &v.Secret)
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
	if v.Secret {
		v.Value = decrypt(s.cipher, v.Value)
	}
	return v, nil
}

func (s *postgresStore) DeleteVariable(ctx context.Context, projectID *uuid.UUID, key string) error {
	var err error
	if projectID == nil {
		_, err = s.db.ExecContext(ctx, `DELETE FROM variables WHERE project_id IS NULL AND key = $1`, key)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM variables WHERE project_id = $1 AND key = $2`, projectID.String(), key)
	}
	return err
}

func (s *postgresStore) CreateExecution(ctx context.Context, e Execution) error {
	if e.Status == "" {
		e.Status = StatusPending
	}
	if e.Trigger == "" {
		e.Trigger = TriggerManual
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO executions (id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
    `, e.ID.String(), e.ProjectID.String(), e.Workflow, e.Ref, e.CommitSHA, e.Status, e.Trigger, e.Steps, e.Error, e.StartedAt, e.FinishedAt, e.CreatedAt, e.ConcurrencyGroup)
	return err
}

func (s *postgresStore) UpdateExecution(ctx context.Context, e Execution) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = $1, steps_json = $2, error = $3, started_at = $4, finished_at = $5, commit_sha = $6, concurrency_group = $7
        WHERE id = $8
    `, e.Status, e.Steps, e.Error, e.StartedAt, e.FinishedAt, e.CommitSHA, e.ConcurrencyGroup, e.ID.String())
	return err
}

func (s *postgresStore) GetExecution(ctx context.Context, id uuid.UUID) (Execution, error) {
	var e Execution
	var idStr, projectIDStr string
	err := s.db.QueryRowContext(ctx, `
        SELECT id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group
        FROM executions
        WHERE id = $1
    `, id.String()).Scan(&idStr, &projectIDStr, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &e.StartedAt, &e.FinishedAt, &e.CreatedAt, &e.ConcurrencyGroup)
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
	return e, nil
}

func (s *postgresStore) ListExecutions(ctx context.Context, projectID uuid.UUID, limit int) ([]Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, project_id, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, finished_at, created_at, concurrency_group
        FROM executions
        WHERE project_id = $1
        ORDER BY created_at DESC
        LIMIT $2
    `, projectID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	executions := make([]Execution, 0)
	for rows.Next() {
		var e Execution
		var idStr, projectIDStr string
		if err := rows.Scan(&idStr, &projectIDStr, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &e.StartedAt, &e.FinishedAt, &e.CreatedAt, &e.ConcurrencyGroup); err != nil {
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
		executions = append(executions, e)
	}
	return executions, rows.Err()
}

func (s *postgresStore) ClaimPendingExecution(ctx context.Context, workerID string) (Execution, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Execution{}, err
	}
	defer tx.Rollback()

	var id string
	if err := tx.QueryRowContext(ctx, `
        SELECT id
        FROM executions
        WHERE status = $1
        ORDER BY created_at ASC
        LIMIT 1
        FOR UPDATE SKIP LOCKED
    `, StatusPending).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	} else if err != nil {
		return Execution{}, err
	}

	result, err := tx.ExecContext(ctx, `
        UPDATE executions
        SET status = $1, claimed_by = $2, started_at = $3
        WHERE id = $4 AND status = $5
    `, StatusRunning, workerID, time.Now(), id, StatusPending)
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

func (s *postgresStore) RequeueOrphanedExecutions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = $1, claimed_by = $2
        WHERE status = $3
    `, StatusPending, "", StatusRunning)
	return err
}

func (s *postgresStore) SetCancelRequested(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE executions SET cancel_requested = true WHERE id = $1`, id.String())
	return err
}

func (s *postgresStore) IsCancelRequested(ctx context.Context, id uuid.UUID) (bool, error) {
	var b bool
	if err := s.db.QueryRowContext(ctx, `SELECT cancel_requested FROM executions WHERE id = $1`, id.String()).Scan(&b); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	return b, nil
}

func (s *postgresStore) CountPendingExecutions(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM executions WHERE status = $1`, StatusPending).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *postgresStore) CancelRunningInGroup(ctx context.Context, projectID uuid.UUID, group string, excludeID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET cancel_requested = true
        WHERE project_id = $1 AND concurrency_group = $2 AND status = $3 AND id != $4
    `, projectID.String(), group, StatusRunning, excludeID.String())
	return err
}

func (s *postgresStore) UpsertSchedule(ctx context.Context, sch Schedule) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO schedules (project_id, workflow, cron_expr, next_run_at)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (project_id, workflow) DO UPDATE SET cron_expr = excluded.cron_expr, next_run_at = excluded.next_run_at
    `, sch.ProjectID.String(), sch.Workflow, sch.CronExpr, sch.NextRunAt)
	return err
}

func (s *postgresStore) DeleteSchedule(ctx context.Context, projectID uuid.UUID, workflow string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE project_id = $1 AND workflow = $2`, projectID.String(), workflow)
	return err
}

func (s *postgresStore) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT project_id, workflow, cron_expr, next_run_at FROM schedules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedules := make([]Schedule, 0)
	for rows.Next() {
		var sch Schedule
		var projectIDStr string
		if err := rows.Scan(&projectIDStr, &sch.Workflow, &sch.CronExpr, &sch.NextRunAt); err != nil {
			return nil, err
		}
		sch.ProjectID, err = uuid.Parse(projectIDStr)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, sch)
	}
	return schedules, rows.Err()
}

func (s *postgresStore) ListDueSchedules(ctx context.Context, now time.Time) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT project_id, workflow, cron_expr, next_run_at
        FROM schedules
        WHERE next_run_at <= $1
    `, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schedules := make([]Schedule, 0)
	for rows.Next() {
		var sch Schedule
		var projectIDStr string
		if err := rows.Scan(&projectIDStr, &sch.Workflow, &sch.CronExpr, &sch.NextRunAt); err != nil {
			return nil, err
		}
		sch.ProjectID, err = uuid.Parse(projectIDStr)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, sch)
	}
	return schedules, rows.Err()
}
