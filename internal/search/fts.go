package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/imyousuf/fs-image-manager/internal/db/store"
)

// The FTS5 index (assets_fts) is hand-written SQL rather than sqlc-generated:
// FTS5's MATCH operator, the special rank ordering, and the dynamic faceting
// joins below are not expressible through sqlc's typed query model. The table is
// content-less (one row per asset, asset_id UNINDEXED), so "update" means delete
// the asset's row and re-insert it.

// ftsRow is the searchable column set of one assets_fts row. search-index fills
// name/camera/lens; ai-people fills labels/caption/ocr/persons later.
type ftsRow struct {
	name    string
	camera  string
	lens    string
	labels  string
	caption string
	ocr     string
	persons string
}

// writeFTS replaces the asset's FTS row with r (delete-then-insert, since FTS5
// content-less tables have no UPSERT).
func writeFTS(ctx context.Context, tx dbExec, assetID string, r ftsRow) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM assets_fts WHERE asset_id = ?`, assetID); err != nil {
		return fmt.Errorf("fts: delete %s: %w", assetID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO assets_fts (asset_id, name, camera, lens, labels, caption, ocr, persons)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		assetID, r.name, r.camera, r.lens, r.labels, r.caption, r.ocr, r.persons); err != nil {
		return fmt.Errorf("fts: insert %s: %w", assetID, err)
	}
	return nil
}

// upsertFTSCore refreshes the search-index-owned columns (name/camera/lens) of an
// asset's FTS row while preserving any ai-people columns already present.
func upsertFTSCore(ctx context.Context, tx dbExec, assetID, name, camera, lens string) error {
	existing, err := readFTS(ctx, tx, assetID)
	if err != nil {
		return err
	}
	existing.name = name
	existing.camera = camera
	existing.lens = lens
	return writeFTS(ctx, tx, assetID, existing)
}

// UpsertEnrichment refreshes ALL ai-people-owned FTS columns (labels/caption/ocr
// /persons) at once for an asset, preserving name/camera/lens. Use this only when
// a single caller produces all four together; ai-people's enrich-ai and
// face-index are SEPARATE jobs writing disjoint columns, so they should use the
// column-scoped UpsertEnrichmentText / UpsertPersons instead to avoid one job
// clobbering the other's text.
func (ix *Index) UpsertEnrichment(ctx context.Context, assetID, labels, caption, ocr, persons string) error {
	return ix.mutateFTS(ctx, assetID, func(r *ftsRow) {
		r.labels = labels
		r.caption = caption
		r.ocr = ocr
		r.persons = persons
	})
}

// UpsertEnrichmentText refreshes only the enrich-ai-owned FTS columns
// (labels/caption/ocr) for an asset, preserving persons (written by face-index)
// and the name/camera/lens core. ai-people's enrich-ai pipeline calls this so a
// caption/label becomes searchable without wiping a named person on the same row.
func (ix *Index) UpsertEnrichmentText(ctx context.Context, assetID, labels, caption, ocr string) error {
	return ix.mutateFTS(ctx, assetID, func(r *ftsRow) {
		r.labels = labels
		r.caption = caption
		r.ocr = ocr
	})
}

// UpsertPersons refreshes only the FTS "persons" column for an asset, preserving
// labels/caption/ocr (written by enrich-ai) and the name/camera/lens core.
// ai-people's face-index pipeline calls this so a named person's photos are
// findable by name without clobbering captions/labels.
func (ix *Index) UpsertPersons(ctx context.Context, assetID, persons string) error {
	return ix.mutateFTS(ctx, assetID, func(r *ftsRow) {
		r.persons = persons
	})
}

// mutateFTS is the read-modify-write core shared by the enrichment mutators: it
// loads the asset's current FTS row, applies apply (which sets only the columns
// that mutator owns), and writes it back -- all in one transaction so concurrent
// column-scoped updates never lose each other's data.
func (ix *Index) mutateFTS(ctx context.Context, assetID string, apply func(*ftsRow)) error {
	return ix.withTx(ctx, func(_ *store.Queries, tx *sql.Tx) error {
		existing, err := readFTS(ctx, tx, assetID)
		if err != nil {
			return err
		}
		apply(&existing)
		return writeFTS(ctx, tx, assetID, existing)
	})
}

// readFTS loads the FTS columns for an asset in the table's declared order,
// returning a zero ftsRow when none exists.
func readFTS(ctx context.Context, tx dbExec, assetID string) (ftsRow, error) {
	var r ftsRow
	row := tx.QueryRowContext(ctx,
		`SELECT name, camera, lens, labels, caption, ocr, persons
		   FROM assets_fts WHERE asset_id = ?`, assetID)
	err := row.Scan(&r.name, &r.camera, &r.lens, &r.labels, &r.caption, &r.ocr, &r.persons)
	switch {
	case err == nil:
		return r, nil
	case errors.Is(err, sql.ErrNoRows):
		return ftsRow{}, nil
	default:
		return ftsRow{}, fmt.Errorf("fts: read %s: %w", assetID, err)
	}
}

// dbExec is the subset of *sql.DB / *sql.Tx the FTS helpers use, so they can run
// inside or outside a transaction.
type dbExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
