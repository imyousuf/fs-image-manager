// Package search makes the library searchable and time-navigable. It owns the
// normalized metadata table, the FTS5 full-text index over an asset's
// name/camera/lens (and the labels/caption/ocr/persons columns ai-people fills
// later), the timeline aggregation, and a pure-Go embedding store for semantic
// search. It also provides the Indexer that ties metadata extraction (package
// internal/metadata) to persistence: ingestion calls IndexAsset, which extracts,
// stores metadata, updates the FTS index and sets the catalog's CapturedAt.
//
// Everything here is additive to browsing: a library with nothing indexed still
// browses and streams; search simply returns no hits until ingest/backfill runs.
package search

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/imyousuf/fs-image-manager/internal/db/store"
)

// Index is the search subsystem over the application database. It uses the sqlc
// store for the normalized metadata + embedding tables and hand-written SQL for
// the FTS5 virtual table (whose MATCH syntax and dynamic faceting are awkward
// through sqlc). It is safe for concurrent use to the extent the underlying
// *sql.DB is (the app caps SQLite to a single writer).
type Index struct {
	db *sql.DB
	q  *store.Queries
}

// New builds an Index over an open database connection (migrations already
// applied by internal/db).
func New(db *sql.DB) *Index {
	return &Index{db: db, q: store.New(db)}
}

// withTx runs fn inside a transaction, committing on success and rolling back on
// error. Used so a metadata upsert and its FTS row move together.
func (ix *Index) withTx(ctx context.Context, fn func(q *store.Queries, tx *sql.Tx) error) (err error) {
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("search: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(ix.q.WithTx(tx), tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("search: commit tx: %w", err)
	}
	return nil
}
