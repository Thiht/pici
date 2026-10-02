//go:build !postgres

package stores

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// By default the store tests run against SQLite. Build them with
// `-tags=postgres` (the test:postgres task does) to run them against Postgres.

func testDriver() string {
	return "sqlite"
}

func testDSN(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func rawTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteDSN(dsn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
