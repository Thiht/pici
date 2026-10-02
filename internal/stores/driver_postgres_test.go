//go:build postgres

package stores

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"uuid"
)

// The store tests then run against the throwaway Postgres started by the
// test:postgres task, on a fixed address. PGHOST and PGPORT override it: a pici
// step runs in its own container, where a port published by the host is out of
// reach, so it points the tests at the container running the database.
const (
	defaultHost = "127.0.0.1"
	defaultPort = "55432"
)

func testDriver() string {
	return "postgres"
}

func baseDSN() string {
	host, port := defaultHost, defaultPort
	if value := os.Getenv("PGHOST"); value != "" {
		host = value
	}
	if value := os.Getenv("PGPORT"); value != "" {
		port = value
	}
	return "postgres://postgres:pici@" + net.JoinHostPort(host, port) + "/pici_test?sslmode=disable"
}

// testDSN creates an empty database on the test server, and drops it when the
// test ends.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := baseDSN()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %q: %v", dsn, err)
	}

	name := "pici_test_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", u.Redacted(), err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(context.Background(), `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		// The store is closed first (cleanups run last in, first out), so the
		// database has no connection left when it is dropped.
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+name+`"`)
	})

	target := *u
	target.Path = "/" + name
	return target.String()
}

func rawTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
