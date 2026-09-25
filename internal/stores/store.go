package stores

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/Thiht/pici/internal/secrets"
)

//go:embed schema.sql
var schemaSQL string

type Store interface {
	CreateProject(ctx context.Context, p Project) (Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	GetProject(ctx context.Context, id string) (Project, error)
	GetProjectByName(ctx context.Context, name string) (Project, error)
	UpdateProject(ctx context.Context, p Project) (Project, error)
	DeleteProject(ctx context.Context, id string) error

	SetVariable(ctx context.Context, v Variable) error
	ListVariables(ctx context.Context, projectID string) ([]Variable, error)
	GetVariable(ctx context.Context, projectID, key string) (Variable, error)
	DeleteVariable(ctx context.Context, projectID, key string) error

	CreateExecution(ctx context.Context, e Execution) error
	UpdateExecution(ctx context.Context, e Execution) error
	GetExecution(ctx context.Context, id string) (Execution, error)
	ListExecutions(ctx context.Context, projectID string, limit int) ([]Execution, error)

	ClaimPendingExecution(ctx context.Context, workerID string) (Execution, error)
	RequeueOrphanedExecutions(ctx context.Context) error
	SetCancelRequested(ctx context.Context, id string) error
	IsCancelRequested(ctx context.Context, id string) (bool, error)
	CountPendingExecutions(ctx context.Context) (int, error)
	CancelRunningInGroup(ctx context.Context, projectID, group, excludeID string) error

	UpsertSchedule(ctx context.Context, s Schedule) error
	DeleteSchedule(ctx context.Context, projectID, workflow string) error
	ListSchedules(ctx context.Context) ([]Schedule, error)
	ListDueSchedules(ctx context.Context, now int64) ([]Schedule, error)

	Close() error
}

var ErrNotFound = errors.New("not found")

func Open(driver, dsn string, cipher *secrets.Cipher) (Store, error) {
	var db *sql.DB
	var err error
	switch driver {
	case "sqlite", "sqlite3":
		db, err = sql.Open("sqlite", sqliteDSN(dsn))
	case "postgres", "postgresql":
		db, err = sql.Open("pgx", dsn)
	default:
		return nil, fmt.Errorf("unsupported db driver %q", driver)
	}
	if err != nil {
		return nil, err
	}

	if err := migrate(db, driver); err != nil {
		db.Close()
		return nil, err
	}

	switch driver {
	case "sqlite", "sqlite3":
		return &sqliteStore{db: db, cipher: cipher}, nil
	default:
		return &postgresStore{db: db, cipher: cipher}, nil
	}
}

func sqliteDSN(dsn string) string {
	if dsn == "" {
		dsn = "pici.db"
	}
	if strings.Contains(dsn, "_pragma=busy_timeout") {
		return dsn
	}
	pragmas := "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	switch {
	case dsn == ":memory:":
		return "file::memory:?cache=shared&" + pragmas
	case strings.HasPrefix(dsn, "file:"):
		if strings.Contains(dsn, "?") {
			return dsn + "&" + pragmas
		}
		return dsn + "?" + pragmas
	default:
		return "file:" + dsn + "?" + pragmas
	}
}

func migrate(db *sql.DB, driver string) error {
	for _, stmt := range strings.Split(schemaSQL, ";") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	additions := []struct{ table, column, ddl string }{
		{"projects", "webhook_secret", `ALTER TABLE projects ADD COLUMN webhook_secret TEXT NOT NULL DEFAULT ''`},
		{"executions", "claimed_by", `ALTER TABLE executions ADD COLUMN claimed_by TEXT NOT NULL DEFAULT ''`},
		{"executions", "cancel_requested", `ALTER TABLE executions ADD COLUMN cancel_requested BIGINT NOT NULL DEFAULT 0`},
		{"executions", "concurrency_group", `ALTER TABLE executions ADD COLUMN concurrency_group TEXT NOT NULL DEFAULT ''`},
	}
	for _, a := range additions {
		exists, err := columnExists(db, driver, a.table, a.column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec(a.ddl); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", a.table, a.column, err)
			}
		}
	}
	return nil
}

func columnExists(db *sql.DB, driver, table, column string) (bool, error) {
	var (
		n   int
		err error
	)
	if driver == "postgres" || driver == "postgresql" {
		err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = $1 AND column_name = $2`, table, column).Scan(&n)
	} else {
		err = db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, table), column).Scan(&n)
	}
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func encrypt(cipher *secrets.Cipher, value string) string {
	if cipher == nil || value == "" {
		return value
	}
	if encrypted, err := cipher.Encrypt(value); err == nil {
		return encrypted
	}
	return value
}

func decrypt(cipher *secrets.Cipher, value string) string {
	if cipher == nil || value == "" {
		return value
	}
	if decrypted, err := cipher.Decrypt(value); err == nil {
		return decrypted
	}
	return ""
}

func marshalSteps(steps []StepResult) (string, error) {
	if len(steps) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(steps)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalSteps(s string) ([]StepResult, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var steps []StepResult
	if err := json.Unmarshal([]byte(s), &steps); err != nil {
		return nil, err
	}
	return steps, nil
}

func scanSchedules(rows *sql.Rows) ([]Schedule, error) {
	schedules := make([]Schedule, 0)
	for rows.Next() {
		var sch Schedule
		if err := rows.Scan(&sch.ProjectID, &sch.Workflow, &sch.CronExpr, &sch.NextRunAt); err != nil {
			return nil, err
		}
		schedules = append(schedules, sch)
	}
	return schedules, rows.Err()
}
