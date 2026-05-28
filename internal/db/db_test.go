package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/db/store"
)

func TestOpenAndMigrate(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fsim.db")

	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Foreign keys must be enabled.
	var fk int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}

	// WAL journal mode must be active.
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	// The platform migration created app_meta with a schema_version row.
	q := store.New(conn)
	v, err := q.GetMeta(ctx, "schema_version")
	if err != nil {
		t.Fatalf("GetMeta(schema_version): %v", err)
	}
	if v != "1" {
		t.Errorf("schema_version = %q, want 1", v)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fsim.db")

	conn, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Re-running migrations on an already-migrated DB must be a no-op.
	if err := db.Migrate(ctx, conn); err != nil {
		t.Fatalf("re-Migrate: %v", err)
	}
}

func TestStoreSetGetMeta(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "fsim.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	q := store.New(conn)
	if err := q.SetMeta(ctx, store.SetMetaParams{Key: "last_scan", Value: "2026-05-28"}); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	got, err := q.GetMeta(ctx, "last_scan")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if got != "2026-05-28" {
		t.Errorf("got %q, want 2026-05-28", got)
	}

	// Upsert overwrites.
	if err := q.SetMeta(ctx, store.SetMetaParams{Key: "last_scan", Value: "2026-06-01"}); err != nil {
		t.Fatalf("SetMeta upsert: %v", err)
	}
	got, _ = q.GetMeta(ctx, "last_scan")
	if got != "2026-06-01" {
		t.Errorf("after upsert got %q, want 2026-06-01", got)
	}
}
