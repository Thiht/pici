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

func (s *sqliteStore) CreateExecution(ctx context.Context, e *Execution) error {
	if e.Status == "" {
		e.Status = StatusPending
	}
	if e.Trigger == "" {
		e.Trigger = TriggerManual
	}
	if e.Source == "" {
		e.Source = SourceGit
	}
	if e.Attempt == 0 {
		e.Attempt = 1
	}
	if e.WorkspaceID == nil {
		e.WorkspaceID = &e.ID
	}
	var startedMs, setupFinishedMs, finishedMs any
	if e.StartedAt != nil {
		startedMs = e.StartedAt.UnixMilli()
	}
	if e.SetupFinishedAt != nil {
		setupFinishedMs = e.SetupFinishedAt.UnixMilli()
	}
	if e.FinishedAt != nil {
		finishedMs = e.FinishedAt.UnixMilli()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `UPDATE projects SET execution_seq = execution_seq + 1 WHERE id = ? RETURNING execution_seq`, e.ProjectID.String()).Scan(&e.ID); err != nil {
		return err
	}
	if e.ExecutionID == 0 {
		e.ExecutionID = e.ID
		e.Attempt = 1
	}
	if e.AttemptsCount == 0 {
		e.AttemptsCount = e.Attempt
	}
	var snapshotID any
	if e.SnapshotID != nil {
		snapshotID = e.SnapshotID.String()
	}
	var createdBy any
	if e.CreatedBy != nil {
		createdBy = e.CreatedBy.String()
	}
	parentID := e.ParentID
	if e.PreviousAttemptID != nil {
		parentID = e.PreviousAttemptID
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO executions (project_id, id, execution_id, attempt, workflow, ref, commit_sha, status, trigger, steps_json, error, started_at, setup_finished_at, finished_at, created_at, concurrency_group, source, snapshot_id, parent_id, workspace_id, created_by, check_run_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, e.ProjectID.String(), e.ID, e.ExecutionID, e.Attempt, e.Workflow, e.Ref, e.CommitSHA, e.Status, e.Trigger, e.Steps, e.Error, startedMs, setupFinishedMs, finishedMs, e.CreatedAt.UnixMilli(), e.ConcurrencyGroup, e.Source, snapshotID, parentID, e.WorkspaceID, createdBy, e.CheckRunID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) UpdateExecution(ctx context.Context, e Execution) error {
	var startedMs, setupFinishedMs, finishedMs any
	if e.StartedAt != nil {
		startedMs = e.StartedAt.UnixMilli()
	}
	if e.SetupFinishedAt != nil {
		setupFinishedMs = e.SetupFinishedAt.UnixMilli()
	}
	if e.FinishedAt != nil {
		finishedMs = e.FinishedAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, steps_json = ?, error = ?, started_at = ?, setup_finished_at = ?, finished_at = ?, commit_sha = ?, concurrency_group = ?
        WHERE project_id = ? AND id = ?
    `, e.Status, e.Steps, e.Error, startedMs, setupFinishedMs, finishedMs, e.CommitSHA, e.ConcurrencyGroup, e.ProjectID.String(), e.ID)
	return err
}

const sqliteRawExecutionColumns = `
    e.project_id, e.id, COALESCE(e.execution_id, e.id), e.attempt, e.workflow, e.ref, e.commit_sha,
    e.status, e.trigger, e.steps_json, e.error, e.started_at, e.setup_finished_at, e.finished_at,
    e.created_at, e.concurrency_group, e.source, e.snapshot_id,
    CASE WHEN e.trigger = 'retry' THEN e.parent_id END,
    CASE WHEN e.trigger = 'rebuild' THEN e.parent_id END,
    e.workspace_id, e.created_by, u.login, e.check_run_id,
    COALESCE((SELECT MAX(a.attempt) FROM executions a WHERE a.project_id = e.project_id AND a.execution_id = e.execution_id), e.attempt),
    e.created_at
`

const sqliteJoinedExecutionColumns = `
    e.project_id, e.id, COALESCE(e.execution_id, e.id), e.attempt, e.workflow, e.ref, e.commit_sha,
    e.status, r.trigger, e.steps_json, e.error, e.started_at, e.setup_finished_at, e.finished_at,
    r.created_at, e.concurrency_group, e.source, e.snapshot_id,
    CASE WHEN e.trigger = 'retry' THEN e.parent_id END, r.parent_id,
    e.workspace_id, r.created_by, u.login, r.check_run_id,
    COALESCE((SELECT MAX(a.attempt) FROM executions a WHERE a.project_id = e.project_id AND a.execution_id = e.execution_id), e.attempt),
    e.created_at
`

func (s *sqliteStore) scanExecution(row rowScanner) (Execution, error) {
	var e Execution
	var projectIDStr string
	var snapshotID *string
	var startedMs, setupFinishedMs, finishedMs *int64
	var createdMs, attemptCreatedMs int64
	var createdBy, createdByLogin *string
	var previousAttemptID, parentID *int64
	if err := row.Scan(&projectIDStr, &e.ID, &e.ExecutionID, &e.Attempt, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &startedMs, &setupFinishedMs, &finishedMs, &createdMs, &e.ConcurrencyGroup, &e.Source, &snapshotID, &previousAttemptID, &parentID, &e.WorkspaceID, &createdBy, &createdByLogin, &e.CheckRunID, &e.AttemptsCount, &attemptCreatedMs); err != nil {
		return Execution{}, err
	}
	e.PreviousAttemptID = previousAttemptID
	e.ParentID = parentID
	var err error
	e.ProjectID, err = uuid.Parse(projectIDStr)
	if err != nil {
		return Execution{}, err
	}
	if e.Attempt == 0 {
		e.Attempt = 1
	}
	if e.AttemptsCount == 0 {
		e.AttemptsCount = e.Attempt
	}
	if createdBy != nil {
		parsed, err := uuid.Parse(*createdBy)
		if err != nil {
			return Execution{}, err
		}
		e.CreatedBy = &parsed
	}
	if createdByLogin != nil {
		e.CreatedByLogin = *createdByLogin
	}
	if snapshotID != nil {
		parsed, err := uuid.Parse(*snapshotID)
		if err != nil {
			return Execution{}, err
		}
		e.SnapshotID = &parsed
	}
	if startedMs != nil {
		t := time.UnixMilli(*startedMs)
		e.StartedAt = &t
	}
	if setupFinishedMs != nil {
		t := time.UnixMilli(*setupFinishedMs)
		e.SetupFinishedAt = &t
	}
	if finishedMs != nil {
		t := time.UnixMilli(*finishedMs)
		e.FinishedAt = &t
	}
	e.CreatedAt = time.UnixMilli(createdMs)
	e.AttemptCreatedAt = time.UnixMilli(attemptCreatedMs)
	return e, nil
}

func (s *sqliteStore) GetAttempt(ctx context.Context, projectID uuid.UUID, id int64) (Execution, error) {
	e, err := s.scanExecution(s.db.QueryRowContext(ctx, `
        SELECT `+sqliteRawExecutionColumns+`
        FROM executions e
        LEFT JOIN users u ON u.id = e.created_by
        WHERE e.project_id = ? AND e.id = ?
    `, projectID.String(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	return e, err
}

func (s *sqliteStore) GetExecutionAttempt(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (Execution, error) {
	query := `
        SELECT ` + sqliteJoinedExecutionColumns + `
        FROM executions e
        JOIN executions r ON r.project_id = e.project_id AND r.id = e.execution_id
        LEFT JOIN users u ON u.id = r.created_by
        WHERE e.project_id = ? AND e.execution_id = ?`
	args := []any{projectID.String(), executionID}
	if attempt > 0 {
		query += ` AND e.attempt = ?`
		args = append(args, attempt)
	} else {
		query += ` ORDER BY e.attempt DESC LIMIT 1`
	}
	e, err := s.scanExecution(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	return e, err
}

func (s *sqliteStore) ListExecutions(ctx context.Context, projectID uuid.UUID, limit int) ([]Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT `+sqliteJoinedExecutionColumns+`
        FROM executions e
        JOIN executions r ON r.project_id = e.project_id AND r.id = e.execution_id
        LEFT JOIN users u ON u.id = r.created_by
        WHERE e.project_id = ?
          AND e.attempt = (
              SELECT MAX(a.attempt) FROM executions a
              WHERE a.project_id = e.project_id AND a.execution_id = e.execution_id
          )
        ORDER BY e.created_at DESC, e.id DESC
        LIMIT ?
    `, projectID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	executions := make([]Execution, 0)
	for rows.Next() {
		e, err := s.scanExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, e)
	}
	return executions, rows.Err()
}

func (s *sqliteStore) ListAttempts(ctx context.Context, projectID uuid.UUID, executionID int64) ([]Execution, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT `+sqliteRawExecutionColumns+`
        FROM executions e
        LEFT JOIN users u ON u.id = e.created_by
        WHERE e.project_id = ? AND e.execution_id = ?
        ORDER BY e.attempt ASC
    `, projectID.String(), executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	attempts := make([]Execution, 0)
	for rows.Next() {
		e, err := s.scanExecution(rows)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, e)
	}
	return attempts, rows.Err()
}

func (s *sqliteStore) SetCheckRunID(ctx context.Context, projectID uuid.UUID, executionID, checkRunID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE executions SET check_run_id = ? WHERE project_id = ? AND id = ?`, checkRunID, projectID.String(), executionID)
	return err
}

func (s *sqliteStore) ClaimPendingExecution(ctx context.Context, workerID string) (Execution, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Execution{}, err
	}
	defer tx.Rollback()

	var projectIDStr string
	var id int64
	if err := tx.QueryRowContext(ctx, `
        SELECT project_id, id
        FROM executions
        WHERE status = ?
        ORDER BY id ASC
        LIMIT 1
    `, StatusPending).Scan(&projectIDStr, &id); errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	} else if err != nil {
		return Execution{}, err
	}

	result, err := tx.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, claimed_by = ?, started_at = ?
        WHERE project_id = ? AND id = ? AND status = ?
    `, StatusRunning, workerID, time.Now().UnixMilli(), projectIDStr, id, StatusPending)
	if err != nil {
		return Execution{}, err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return Execution{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return Execution{}, err
	}

	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return Execution{}, err
	}
	return s.GetAttempt(ctx, projectID, id)
}

func (s *sqliteStore) RequeueOrphanedExecutions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = ?, claimed_by = ?
        WHERE status = ?
    `, StatusPending, "", StatusRunning)
	return err
}

func (s *sqliteStore) SetCancelRequested(ctx context.Context, projectID uuid.UUID, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE executions SET cancel_requested = 1 WHERE project_id = ? AND id = ?`, projectID.String(), id)
	return err
}

func (s *sqliteStore) IsCancelRequested(ctx context.Context, projectID uuid.UUID, id int64) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT cancel_requested FROM executions WHERE project_id = ? AND id = ?`, projectID.String(), id).Scan(&n); errors.Is(err, sql.ErrNoRows) {
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

func (s *sqliteStore) CancelRunningInGroup(ctx context.Context, projectID uuid.UUID, group string, excludeID int64) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET cancel_requested = 1
        WHERE project_id = ? AND concurrency_group = ? AND status = ? AND id != ?
	`, projectID.String(), group, StatusRunning, excludeID)
	return err
}

func (s *sqliteStore) WorkspaceBusy(ctx context.Context, projectID uuid.UUID, workspaceID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
        SELECT COUNT(*) FROM executions
        WHERE project_id = ? AND workspace_id = ? AND status IN (?, ?)
    `, projectID.String(), workspaceID, StatusPending, StatusRunning).Scan(&n)
	return n > 0, err
}

func (s *sqliteStore) WorkspaceRetained(ctx context.Context, projectID uuid.UUID, workspaceID int64, cutoff time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
        SELECT COUNT(*) FROM executions
        WHERE project_id = ? AND workspace_id = ?
          AND (status IN (?, ?) OR finished_at IS NULL OR finished_at >= ?)
    `, projectID.String(), workspaceID, StatusPending, StatusRunning, cutoff.UnixMilli()).Scan(&n)
	return n > 0, err
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

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *sqliteStore) scanUser(row rowScanner) (User, error) {
	var u User
	var id string
	var createdMs, lastLoginMs int64
	var lastCheckMs, expiresMs, disabledMs *int64
	if err := row.Scan(&id, &u.Provider, &u.ProviderUserID, &u.Login, &u.Name, &u.AvatarURL, &u.OAuthAccessToken, &u.OAuthRefreshToken, &expiresMs, &createdMs, &lastLoginMs, &lastCheckMs, &disabledMs); err != nil {
		return User{}, err
	}
	var err error
	u.ID, err = uuid.Parse(id)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt = time.UnixMilli(createdMs)
	u.LastLoginAt = time.UnixMilli(lastLoginMs)
	if expiresMs != nil {
		t := time.UnixMilli(*expiresMs)
		u.OAuthExpiresAt = &t
	}
	if lastCheckMs != nil {
		t := time.UnixMilli(*lastCheckMs)
		u.LastOrgCheckAt = &t
	}
	if disabledMs != nil {
		t := time.UnixMilli(*disabledMs)
		u.DisabledAt = &t
	}
	u.OAuthAccessToken = decrypt(s.cipher, u.OAuthAccessToken)
	u.OAuthRefreshToken = decrypt(s.cipher, u.OAuthRefreshToken)
	return u, nil
}

func (s *sqliteStore) UpsertUser(ctx context.Context, u User) (User, error) {
	if u.ID == uuid.Nil() {
		u.ID = uuid.New()
	}
	created := u.CreatedAt
	if created.IsZero() {
		created = u.LastLoginAt
	}
	if created.IsZero() {
		created = time.Now()
	}
	lastLogin := u.LastLoginAt
	if lastLogin.IsZero() {
		lastLogin = created
	}
	var expires, lastCheck, disabled any
	if u.OAuthExpiresAt != nil {
		expires = u.OAuthExpiresAt.UnixMilli()
	}
	if u.LastOrgCheckAt != nil {
		lastCheck = u.LastOrgCheckAt.UnixMilli()
	}
	if u.DisabledAt != nil {
		disabled = u.DisabledAt.UnixMilli()
	}
	accessToken := encrypt(s.cipher, u.OAuthAccessToken)
	refreshToken := encrypt(s.cipher, u.OAuthRefreshToken)

	var id string
	err := s.db.QueryRowContext(ctx, `
        INSERT INTO users (id, provider, provider_user_id, login, name, avatar_url, oauth_token, oauth_refresh_token, oauth_token_expires_at, created_at, last_login_at, last_org_check_at, disabled_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (provider, provider_user_id) DO UPDATE SET
            login = excluded.login,
            name = excluded.name,
            avatar_url = excluded.avatar_url,
            oauth_token = excluded.oauth_token,
            oauth_refresh_token = CASE WHEN excluded.oauth_refresh_token = '' THEN users.oauth_refresh_token ELSE excluded.oauth_refresh_token END,
            oauth_token_expires_at = excluded.oauth_token_expires_at,
            last_login_at = excluded.last_login_at,
            last_org_check_at = excluded.last_org_check_at,
            disabled_at = NULL
        RETURNING id
    `, u.ID.String(), u.Provider, u.ProviderUserID, u.Login, u.Name, u.AvatarURL, accessToken, refreshToken, expires, created.UnixMilli(), lastLogin.UnixMilli(), lastCheck, disabled).Scan(&id)
	if err != nil {
		return User{}, err
	}
	u.ID, err = uuid.Parse(id)
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, u.ID)
}

func (s *sqliteStore) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := s.scanUser(s.db.QueryRowContext(ctx, `
        SELECT id, provider, provider_user_id, login, name, avatar_url, oauth_token, oauth_refresh_token, oauth_token_expires_at, created_at, last_login_at, last_org_check_at, disabled_at
        FROM users
        WHERE id = ?
    `, id.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *sqliteStore) ListActiveUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, provider, provider_user_id, login, name, avatar_url, oauth_token, oauth_refresh_token, oauth_token_expires_at, created_at, last_login_at, last_org_check_at, disabled_at
        FROM users
        WHERE disabled_at IS NULL
        ORDER BY login
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *sqliteStore) UpdateUserOAuthToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt *time.Time) error {
	var expires any
	if expiresAt != nil {
		expires = expiresAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
        UPDATE users SET oauth_token = ?, oauth_refresh_token = ?, oauth_token_expires_at = ? WHERE id = ?
    `, encrypt(s.cipher, accessToken), encrypt(s.cipher, refreshToken), expires, id.String())
	return err
}

func (s *sqliteStore) SetUserOrgCheck(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_org_check_at = ? WHERE id = ?`, at.UnixMilli(), id.String())
	return err
}

func (s *sqliteStore) DisableUser(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, at.UnixMilli(), id.String())
	return err
}

func (s *sqliteStore) scanSession(row rowScanner) (Session, error) {
	var sess Session
	var id, userID string
	var createdMs, seenMs, expiresMs int64
	if err := row.Scan(&id, &userID, &sess.CSRFToken, &createdMs, &seenMs, &expiresMs, &sess.UserAgent); err != nil {
		return Session{}, err
	}
	sess.ID = id
	uid, err := uuid.Parse(userID)
	if err != nil {
		return Session{}, err
	}
	sess.UserID = uid
	sess.CreatedAt = time.UnixMilli(createdMs)
	sess.LastSeenAt = time.UnixMilli(seenMs)
	sess.ExpiresAt = time.UnixMilli(expiresMs)
	return sess, nil
}

func (s *sqliteStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO sessions (id, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, sess.ID, sess.UserID.String(), sess.CSRFToken, sess.CreatedAt.UnixMilli(), sess.LastSeenAt.UnixMilli(), sess.ExpiresAt.UnixMilli(), sess.UserAgent)
	return err
}

func (s *sqliteStore) GetSession(ctx context.Context, id string) (Session, error) {
	sess, err := s.scanSession(s.db.QueryRowContext(ctx, `
        SELECT id, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent
        FROM sessions
        WHERE id = ?
    `, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *sqliteStore) TouchSession(ctx context.Context, id string, seen, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, seen.UnixMilli(), expires.UnixMilli(), id)
	return err
}

func (s *sqliteStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *sqliteStore) DeleteUserSession(ctx context.Context, userID uuid.UUID, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id = ?`, userID.String(), id)
	return err
}

func (s *sqliteStore) DeleteUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID.String())
	return err
}

func (s *sqliteStore) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, before.UnixMilli())
	return err
}

func (s *sqliteStore) ListUserSessions(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent
        FROM sessions
        WHERE user_id = ?
        ORDER BY last_seen_at DESC
    `, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]Session, 0)
	for rows.Next() {
		sess, err := s.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *sqliteStore) scanAPIToken(row rowScanner) (APIToken, error) {
	var t APIToken
	var id, userID string
	var createdMs int64
	var lastUsedMs, expiresMs *int64
	if err := row.Scan(&id, &userID, &t.Name, &t.Prefix, &t.TokenHash, &createdMs, &lastUsedMs, &expiresMs); err != nil {
		return APIToken{}, err
	}
	var err error
	t.ID, err = uuid.Parse(id)
	if err != nil {
		return APIToken{}, err
	}
	t.UserID, err = uuid.Parse(userID)
	if err != nil {
		return APIToken{}, err
	}
	t.CreatedAt = time.UnixMilli(createdMs)
	if lastUsedMs != nil {
		v := time.UnixMilli(*lastUsedMs)
		t.LastUsedAt = &v
	}
	if expiresMs != nil {
		v := time.UnixMilli(*expiresMs)
		t.ExpiresAt = &v
	}
	return t, nil
}

func (s *sqliteStore) CreateAPIToken(ctx context.Context, t APIToken) error {
	var lastUsed, expires any
	if t.LastUsedAt != nil {
		lastUsed = t.LastUsedAt.UnixMilli()
	}
	if t.ExpiresAt != nil {
		expires = t.ExpiresAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO api_tokens (id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, t.ID.String(), t.UserID.String(), t.Name, t.Prefix, t.TokenHash, t.CreatedAt.UnixMilli(), lastUsed, expires)
	return err
}

func (s *sqliteStore) GetAPITokenByHash(ctx context.Context, hash string) (APIToken, error) {
	t, err := s.scanAPIToken(s.db.QueryRowContext(ctx, `
        SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at
        FROM api_tokens
        WHERE token_hash = ?
    `, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	return t, err
}

func (s *sqliteStore) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at
        FROM api_tokens
        WHERE user_id = ?
        ORDER BY created_at DESC
    `, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := make([]APIToken, 0)
	for rows.Next() {
		t, err := s.scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *sqliteStore) RenameAPIToken(ctx context.Context, userID, id uuid.UUID, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET name = ? WHERE user_id = ? AND id = ?`, name, userID.String(), id.String())
	return err
}

func (s *sqliteStore) DeleteAPIToken(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE user_id = ? AND id = ?`, userID.String(), id.String())
	return err
}

func (s *sqliteStore) TouchAPIToken(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, at.UnixMilli(), id.String())
	return err
}
