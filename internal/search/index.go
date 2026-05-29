package search

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db/store"
	"github.com/imyousuf/fs-image-manager/internal/metadata"
)

// timeLayout matches catalog.timeLayout (RFC3339Nano) so capture times round-trip
// identically between the metadata table, the FTS index and the catalog.
const timeLayout = time.RFC3339Nano

// CaptureSetter records an asset's capture time on the catalog so the browse
// view shows it. *catalog.Repo satisfies this via SetCapturedAt; it is an
// interface here to keep the package testable and decoupled from the repo.
type CaptureSetter interface {
	SetCapturedAt(ctx context.Context, id string, t time.Time) error
}

// Indexer extracts metadata for an asset and indexes it: it persists the
// normalized metadata row, rebuilds the asset's FTS row from name + camera +
// lens, and propagates the capture time to the catalog. It satisfies
// internal/ingest.Indexer (IndexAsset(ctx, catalog.Asset) error) so the ingester
// drives it as a post-upsert step.
type Indexer struct {
	ix        *Index
	extractor metadata.Extractor
	captures  CaptureSetter
}

// NewIndexer builds an Indexer. captures may be nil (capture time then lives only
// in the metadata table, not echoed onto the catalog asset).
func NewIndexer(ix *Index, extractor metadata.Extractor, captures CaptureSetter) *Indexer {
	return &Indexer{ix: ix, extractor: extractor, captures: captures}
}

// IndexAsset extracts and indexes metadata for a freshly upserted asset. It is
// idempotent: re-indexing the same asset overwrites its metadata and FTS rows.
// Extraction yielding no metadata (SourceNone) still refreshes the FTS row from
// the asset's name so filename search works without EXIF.
func (idx *Indexer) IndexAsset(ctx context.Context, a catalog.Asset) error {
	meta, err := idx.extractor.Extract(ctx, a)
	if err != nil {
		return fmt.Errorf("search: extract %s: %w", a.ID, err)
	}

	if err := idx.ix.PutMetadata(ctx, a, meta); err != nil {
		return err
	}

	if meta.CapturedAt != nil && idx.captures != nil {
		if err := idx.captures.SetCapturedAt(ctx, a.ID, *meta.CapturedAt); err != nil {
			return fmt.Errorf("search: set captured_at %s: %w", a.ID, err)
		}
	}
	return nil
}

// PutMetadata persists the normalized metadata row and refreshes the FTS index
// row for the asset, both in one transaction so search never sees a metadata row
// without its matching FTS entry. The FTS labels/caption/ocr/persons columns are
// preserved across re-index when ai-people has already filled them.
func (ix *Index) PutMetadata(ctx context.Context, a catalog.Asset, m metadata.Meta) error {
	return ix.withTx(ctx, func(q *store.Queries, tx *sql.Tx) error {
		if err := q.UpsertMetadata(ctx, metadataParams(a.ID, m)); err != nil {
			return fmt.Errorf("search: upsert metadata %s: %w", a.ID, err)
		}
		if err := upsertFTSCore(ctx, tx, a.ID, a.BaseName, m.CameraLabel(), m.Lens); err != nil {
			return fmt.Errorf("search: index fts %s: %w", a.ID, err)
		}
		return nil
	})
}

// metadataParams maps a Meta into the sqlc upsert params. Capture time is stored
// in UTC RFC3339Nano; GPS pointers pass through as nullable REALs.
func metadataParams(assetID string, m metadata.Meta) store.UpsertMetadataParams {
	p := store.UpsertMetadataParams{
		AssetID:     assetID,
		CameraMake:  m.CameraMake,
		CameraModel: m.CameraModel,
		Lens:        m.Lens,
		Width:       int64(m.Width),
		Height:      int64(m.Height),
		Orientation: int64(m.Orientation),
		DurationMs:  m.DurationMs,
		Codec:       m.Codec,
		Source:      string(m.Source),
	}
	if m.CapturedAt != nil {
		s := m.CapturedAt.UTC().Format(timeLayout)
		p.CapturedAt = &s
	}
	p.GpsLat = m.GPSLat
	p.GpsLng = m.GPSLng
	return p
}

// GetMetadata returns the stored metadata for an asset as a metadata.Meta, or
// ok=false when none is indexed.
func (ix *Index) GetMetadata(ctx context.Context, assetID string) (metadata.Meta, bool, error) {
	row, err := ix.q.GetMetadata(ctx, assetID)
	if err != nil {
		if isNoRows(err) {
			return metadata.Meta{}, false, nil
		}
		return metadata.Meta{}, false, fmt.Errorf("search: get metadata %s: %w", assetID, err)
	}
	return metaFromRow(row), true, nil
}

func metaFromRow(row store.Metadata) metadata.Meta {
	m := metadata.Meta{
		CameraMake:  row.CameraMake,
		CameraModel: row.CameraModel,
		Lens:        row.Lens,
		Width:       int(row.Width),
		Height:      int(row.Height),
		Orientation: int(row.Orientation),
		DurationMs:  row.DurationMs,
		Codec:       row.Codec,
		Source:      metadata.Source(row.Source),
		GPSLat:      row.GpsLat,
		GPSLng:      row.GpsLng,
	}
	if row.CapturedAt != nil {
		if t, err := time.Parse(timeLayout, *row.CapturedAt); err == nil {
			m.CapturedAt = &t
		}
	}
	return m
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
