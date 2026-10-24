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

func (s *postgresStore) CreateExecution(ctx context.Context, e *Execution) error {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `UPDATE projects SET execution_seq = execution_seq + 1 WHERE id = $1 RETURNING execution_seq`, e.ProjectID.String()).Scan(&e.ID); err != nil {
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
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
    `, e.ProjectID.String(), e.ID, e.ExecutionID, e.Attempt, e.Workflow, e.Ref, e.CommitSHA, e.Status, e.Trigger, e.Steps, e.Error, e.StartedAt, e.SetupFinishedAt, e.FinishedAt, e.CreatedAt, e.ConcurrencyGroup, e.Source, snapshotID, parentID, e.WorkspaceID, createdBy, e.CheckRunID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *postgresStore) UpdateExecution(ctx context.Context, e Execution) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = $1, steps_json = $2, error = $3, started_at = $4, setup_finished_at = $5, finished_at = $6, commit_sha = $7, concurrency_group = $8
        WHERE project_id = $9 AND id = $10
    `, e.Status, e.Steps, e.Error, e.StartedAt, e.SetupFinishedAt, e.FinishedAt, e.CommitSHA, e.ConcurrencyGroup, e.ProjectID.String(), e.ID)
	return err
}

const postgresRawExecutionColumns = `
    e.project_id, e.id, COALESCE(e.execution_id, e.id), e.attempt, e.workflow, e.ref, e.commit_sha,
    e.status, e.trigger, e.steps_json, e.error, e.started_at, e.setup_finished_at, e.finished_at,
    e.created_at, e.concurrency_group, e.source, e.snapshot_id,
    CASE WHEN e.trigger = 'retry' THEN e.parent_id END,
    CASE WHEN e.trigger = 'rebuild' THEN e.parent_id END,
    e.workspace_id, e.created_by, u.login, e.check_run_id,
    COALESCE((SELECT MAX(a.attempt) FROM executions a WHERE a.project_id = e.project_id AND a.execution_id = e.execution_id), e.attempt),
    e.created_at
`

const postgresJoinedExecutionColumns = `
    e.project_id, e.id, COALESCE(e.execution_id, e.id), e.attempt, e.workflow, e.ref, e.commit_sha,
    e.status, r.trigger, e.steps_json, e.error, e.started_at, e.setup_finished_at, e.finished_at,
    r.created_at, e.concurrency_group, e.source, e.snapshot_id,
    CASE WHEN e.trigger = 'retry' THEN e.parent_id END, r.parent_id,
    e.workspace_id, r.created_by, u.login, r.check_run_id,
    COALESCE((SELECT MAX(a.attempt) FROM executions a WHERE a.project_id = e.project_id AND a.execution_id = e.execution_id), e.attempt),
    e.created_at
`

func (s *postgresStore) scanExecution(row rowScanner) (Execution, error) {
	var e Execution
	var projectIDStr string
	var snapshotID *string
	var createdBy, createdByLogin *string
	var previousAttemptID, parentID *int64
	if err := row.Scan(&projectIDStr, &e.ID, &e.ExecutionID, &e.Attempt, &e.Workflow, &e.Ref, &e.CommitSHA, &e.Status, &e.Trigger, &e.Steps, &e.Error, &e.StartedAt, &e.SetupFinishedAt, &e.FinishedAt, &e.CreatedAt, &e.ConcurrencyGroup, &e.Source, &snapshotID, &previousAttemptID, &parentID, &e.WorkspaceID, &createdBy, &createdByLogin, &e.CheckRunID, &e.AttemptsCount, &e.AttemptCreatedAt); err != nil {
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
	return e, nil
}

func (s *postgresStore) GetAttempt(ctx context.Context, projectID uuid.UUID, id int64) (Execution, error) {
	e, err := s.scanExecution(s.db.QueryRowContext(ctx, `
        SELECT `+postgresRawExecutionColumns+`
        FROM executions e
        LEFT JOIN users u ON u.id = e.created_by
        WHERE e.project_id = $1 AND e.id = $2
    `, projectID.String(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	}
	return e, err
}

func (s *postgresStore) GetExecutionAttempt(ctx context.Context, projectID uuid.UUID, executionID int64, attempt int) (Execution, error) {
	query := `
        SELECT ` + postgresJoinedExecutionColumns + `
        FROM executions e
        JOIN executions r ON r.project_id = e.project_id AND r.id = e.execution_id
        LEFT JOIN users u ON u.id = r.created_by
        WHERE e.project_id = $1 AND e.execution_id = $2`
	args := []any{projectID.String(), executionID}
	if attempt > 0 {
		query += ` AND e.attempt = $3`
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

func (s *postgresStore) ListExecutions(ctx context.Context, projectID uuid.UUID, limit int) ([]Execution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT `+postgresJoinedExecutionColumns+`
        FROM executions e
        JOIN executions r ON r.project_id = e.project_id AND r.id = e.execution_id
        LEFT JOIN users u ON u.id = r.created_by
        WHERE e.project_id = $1
          AND e.attempt = (
              SELECT MAX(a.attempt) FROM executions a
              WHERE a.project_id = e.project_id AND a.execution_id = e.execution_id
          )
        ORDER BY e.created_at DESC, e.id DESC
        LIMIT $2
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

func (s *postgresStore) ListAttempts(ctx context.Context, projectID uuid.UUID, executionID int64) ([]Execution, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT `+postgresRawExecutionColumns+`
        FROM executions e
        LEFT JOIN users u ON u.id = e.created_by
        WHERE e.project_id = $1 AND e.execution_id = $2
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

func (s *postgresStore) SetCheckRunID(ctx context.Context, projectID uuid.UUID, executionID, checkRunID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE executions SET check_run_id = $1 WHERE project_id = $2 AND id = $3`, checkRunID, projectID.String(), executionID)
	return err
}

func (s *postgresStore) ClaimPendingExecution(ctx context.Context, workerID string) (Execution, error) {
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
        WHERE status = $1
        ORDER BY id ASC
        LIMIT 1
        FOR UPDATE SKIP LOCKED
    `, StatusPending).Scan(&projectIDStr, &id); errors.Is(err, sql.ErrNoRows) {
		return Execution{}, ErrNotFound
	} else if err != nil {
		return Execution{}, err
	}

	result, err := tx.ExecContext(ctx, `
        UPDATE executions
        SET status = $1, claimed_by = $2, started_at = $3
        WHERE project_id = $4 AND id = $5 AND status = $6
    `, StatusRunning, workerID, time.Now(), projectIDStr, id, StatusPending)
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

func (s *postgresStore) RequeueOrphanedExecutions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET status = $1, claimed_by = $2
        WHERE status = $3
    `, StatusPending, "", StatusRunning)
	return err
}

func (s *postgresStore) SetCancelRequested(ctx context.Context, projectID uuid.UUID, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE executions SET cancel_requested = true WHERE project_id = $1 AND id = $2`, projectID.String(), id)
	return err
}

func (s *postgresStore) IsCancelRequested(ctx context.Context, projectID uuid.UUID, id int64) (bool, error) {
	var b bool
	if err := s.db.QueryRowContext(ctx, `SELECT cancel_requested FROM executions WHERE project_id = $1 AND id = $2`, projectID.String(), id).Scan(&b); errors.Is(err, sql.ErrNoRows) {
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

func (s *postgresStore) CancelRunningInGroup(ctx context.Context, projectID uuid.UUID, group string, excludeID int64) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE executions
        SET cancel_requested = true
        WHERE project_id = $1 AND concurrency_group = $2 AND status = $3 AND id != $4
	`, projectID.String(), group, StatusRunning, excludeID)
	return err
}

func (s *postgresStore) WorkspaceBusy(ctx context.Context, projectID uuid.UUID, workspaceID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
        SELECT COUNT(*) FROM executions
        WHERE project_id = $1 AND workspace_id = $2 AND status IN ($3, $4)
    `, projectID.String(), workspaceID, StatusPending, StatusRunning).Scan(&n)
	return n > 0, err
}

func (s *postgresStore) WorkspaceRetained(ctx context.Context, projectID uuid.UUID, workspaceID int64, cutoff time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
        SELECT COUNT(*) FROM executions
        WHERE project_id = $1 AND workspace_id = $2
          AND (status IN ($3, $4) OR finished_at IS NULL OR finished_at >= $5)
    `, projectID.String(), workspaceID, StatusPending, StatusRunning, cutoff).Scan(&n)
	return n > 0, err
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

func (s *postgresStore) scanUser(row rowScanner) (User, error) {
	var u User
	var id string
	if err := row.Scan(&id, &u.Provider, &u.ProviderUserID, &u.Login, &u.Name, &u.AvatarURL, &u.OAuthAccessToken, &u.OAuthRefreshToken, &u.OAuthExpiresAt, &u.CreatedAt, &u.LastLoginAt, &u.LastOrgCheckAt, &u.DisabledAt); err != nil {
		return User{}, err
	}
	var err error
	u.ID, err = uuid.Parse(id)
	if err != nil {
		return User{}, err
	}
	u.OAuthAccessToken = decrypt(s.cipher, u.OAuthAccessToken)
	u.OAuthRefreshToken = decrypt(s.cipher, u.OAuthRefreshToken)
	return u, nil
}

func (s *postgresStore) UpsertUser(ctx context.Context, u User) (User, error) {
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
	accessToken := encrypt(s.cipher, u.OAuthAccessToken)
	refreshToken := encrypt(s.cipher, u.OAuthRefreshToken)

	var id string
	err := s.db.QueryRowContext(ctx, `
        INSERT INTO users (id, provider, provider_user_id, login, name, avatar_url, oauth_token, oauth_refresh_token, oauth_token_expires_at, created_at, last_login_at, last_org_check_at, disabled_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
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
    `, u.ID.String(), u.Provider, u.ProviderUserID, u.Login, u.Name, u.AvatarURL, accessToken, refreshToken, u.OAuthExpiresAt, created, lastLogin, u.LastOrgCheckAt, u.DisabledAt).Scan(&id)
	if err != nil {
		return User{}, err
	}
	u.ID, err = uuid.Parse(id)
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, u.ID)
}

func (s *postgresStore) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := s.scanUser(s.db.QueryRowContext(ctx, `
        SELECT id, provider, provider_user_id, login, name, avatar_url, oauth_token, oauth_refresh_token, oauth_token_expires_at, created_at, last_login_at, last_org_check_at, disabled_at
        FROM users
        WHERE id = $1
    `, id.String()))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *postgresStore) ListActiveUsers(ctx context.Context) ([]User, error) {
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

func (s *postgresStore) UpdateUserOAuthToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt *time.Time) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE users SET oauth_token = $1, oauth_refresh_token = $2, oauth_token_expires_at = $3 WHERE id = $4
    `, encrypt(s.cipher, accessToken), encrypt(s.cipher, refreshToken), expiresAt, id.String())
	return err
}

func (s *postgresStore) SetUserOrgCheck(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_org_check_at = $1 WHERE id = $2`, at, id.String())
	return err
}

func (s *postgresStore) DisableUser(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled_at = $1 WHERE id = $2`, at, id.String())
	return err
}

func (s *postgresStore) scanSession(row rowScanner) (Session, error) {
	var sess Session
	var userID string
	if err := row.Scan(&sess.ID, &userID, &sess.CSRFToken, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &sess.UserAgent); err != nil {
		return Session{}, err
	}
	uid, err := uuid.Parse(userID)
	if err != nil {
		return Session{}, err
	}
	sess.UserID = uid
	return sess, nil
}

func (s *postgresStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO sessions (id, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, sess.ID, sess.UserID.String(), sess.CSRFToken, sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt, sess.UserAgent)
	return err
}

func (s *postgresStore) GetSession(ctx context.Context, id string) (Session, error) {
	sess, err := s.scanSession(s.db.QueryRowContext(ctx, `
        SELECT id, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent
        FROM sessions
        WHERE id = $1
    `, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *postgresStore) TouchSession(ctx context.Context, id string, seen, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at = $1, expires_at = $2 WHERE id = $3`, seen, expires, id)
	return err
}

func (s *postgresStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (s *postgresStore) DeleteUserSession(ctx context.Context, userID uuid.UUID, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id = $2`, userID.String(), id)
	return err
}

func (s *postgresStore) DeleteUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID.String())
	return err
}

func (s *postgresStore) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < $1`, before)
	return err
}

func (s *postgresStore) ListUserSessions(ctx context.Context, userID uuid.UUID) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, user_id, csrf_token, created_at, last_seen_at, expires_at, user_agent
        FROM sessions
        WHERE user_id = $1
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

func (s *postgresStore) scanAPIToken(row rowScanner) (APIToken, error) {
	var t APIToken
	var id, userID string
	if err := row.Scan(&id, &userID, &t.Name, &t.Prefix, &t.TokenHash, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt); err != nil {
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
	return t, nil
}

func (s *postgresStore) CreateAPIToken(ctx context.Context, t APIToken) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO api_tokens (id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `, t.ID.String(), t.UserID.String(), t.Name, t.Prefix, t.TokenHash, t.CreatedAt, t.LastUsedAt, t.ExpiresAt)
	return err
}

func (s *postgresStore) GetAPITokenByHash(ctx context.Context, hash string) (APIToken, error) {
	t, err := s.scanAPIToken(s.db.QueryRowContext(ctx, `
        SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at
        FROM api_tokens
        WHERE token_hash = $1
    `, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	return t, err
}

func (s *postgresStore) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at
        FROM api_tokens
        WHERE user_id = $1
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

func (s *postgresStore) RenameAPIToken(ctx context.Context, userID, id uuid.UUID, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET name = $1 WHERE user_id = $2 AND id = $3`, name, userID.String(), id.String())
	return err
}

func (s *postgresStore) DeleteAPIToken(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE user_id = $1 AND id = $2`, userID.String(), id.String())
	return err
}

func (s *postgresStore) TouchAPIToken(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = $1 WHERE id = $2`, at, id.String())
	return err
}
