// Package db opens the application's SQLite database (pure-Go modernc driver)
// with WAL and foreign keys enabled, and applies the embedded goose migrations
// at startup. Generated, type-checked query methods live in the sibling
// internal/db/store package (produced by sqlc).
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/imyousuf/fs-image-manager/internal/db/migrations"

	// Pure-Go SQLite driver. Registers the "sqlite" driver name.
	_ "modernc.org/sqlite"
)

// dsnPragmas turns on WAL journaling, foreign-key enforcement and a busy
// timeout so the single-writer model degrades to waiting rather than
// "database is locked" errors. modernc accepts PRAGMAs as `_pragma` query
// params on the DSN.
const dsnPragmas = "?_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=synchronous(NORMAL)"

// Open opens (creating if necessary) the SQLite database at path, applies all
// embedded migrations, and returns the live *sql.DB. The caller owns Close.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", path+dsnPragmas)
	if err != nil {
		return nil, fmt.Errorf("db: open %q: %w", path, err)
	}
	// modernc.org/sqlite is a single-file embedded DB; the WAL single-writer
	// model means we cap to one open connection to serialise writes cleanly.
	conn.SetMaxOpenConns(1)
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("db: ping %q: %w", path, err)
	}
	if err := Migrate(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// Migrate applies all embedded goose migrations to conn (idempotent: already
// applied migrations are skipped).
func Migrate(ctx context.Context, conn *sql.DB) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("db: set goose dialect: %w", err)
	}
	// Quiet goose's default stdout logging; failures still surface via error.
	goose.SetLogger(goose.NopLogger())
	if err := goose.UpContext(ctx, conn, "."); err != nil {
		return fmt.Errorf("db: apply migrations: %w", err)
	}
	return nil
}
