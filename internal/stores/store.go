package stores

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"uuid"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/Thiht/pici/internal/secrets"
)

//go:embed migrations/*/*.sql
var migrationsFS embed.FS

type Store interface {
	CreateProject(ctx context.Context, p Project) (Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	GetProject(ctx context.Context, id uuid.UUID) (Project, error)
	GetProjectByName(ctx context.Context, name string) (Project, error)
	UpdateProject(ctx context.Context, p Project) (Project, error)
	DeleteProject(ctx context.Context, id uuid.UUID) error

	SetVariable(ctx context.Context, v Variable) error
	ListVariables(ctx context.Context, projectID *uuid.UUID) ([]Variable, error)
	GetVariable(ctx context.Context, projectID *uuid.UUID, key string) (Variable, error)
	DeleteVariable(ctx context.Context, projectID *uuid.UUID, key string) error

	CreateExecution(ctx context.Context, e Execution) error
	UpdateExecution(ctx context.Context, e Execution) error
	GetExecution(ctx context.Context, id uuid.UUID) (Execution, error)
	ListExecutions(ctx context.Context, projectID uuid.UUID, limit int) ([]Execution, error)

	ClaimPendingExecution(ctx context.Context, workerID string) (Execution, error)
	RequeueOrphanedExecutions(ctx context.Context) error
	SetCancelRequested(ctx context.Context, id uuid.UUID) error
	IsCancelRequested(ctx context.Context, id uuid.UUID) (bool, error)
	CountPendingExecutions(ctx context.Context) (int, error)
	CancelRunningInGroup(ctx context.Context, projectID uuid.UUID, group string, excludeID uuid.UUID) error

	UpsertSchedule(ctx context.Context, s Schedule) error
	DeleteSchedule(ctx context.Context, projectID uuid.UUID, workflow string) error
	ListSchedules(ctx context.Context) ([]Schedule, error)
	ListDueSchedules(ctx context.Context, now time.Time) ([]Schedule, error)

	Close() error
}

var ErrNotFound = errors.New("not found")

func Open(driver, dsn string, cipher *secrets.Cipher) (Store, error) {
	var (
		db      *sql.DB
		err     error
		dialect string
	)
	switch driver {
	case "sqlite", "sqlite3":
		dialect = "sqlite"
		db, err = sql.Open("sqlite", sqliteDSN(dsn))
	case "postgres", "postgresql":
		dialect = "postgres"
		db, err = sql.Open("pgx", dsn)
	default:
		return nil, fmt.Errorf("unsupported db driver %q", driver)
	}
	if err != nil {
		return nil, err
	}

	if err := migrate(db, dialect); err != nil {
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
	pragmas := "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
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

// migrate applies the embedded schema migrations with goose. Each dialect has
// its own migration directory (migrations/sqlite, migrations/postgres) because
// the schemas use driver-specific types (enums, timestamptz, uuid, ...).
func migrate(db *sql.DB, dialect string) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := goose.Up(db, path.Join("migrations", dialect)); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
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
