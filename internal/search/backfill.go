package search

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// AssetLister enumerates the catalog's assets for a backfill pass. *catalog.Repo
// satisfies it via ListAllAssets; it is an interface so the backfill is testable
// against a fake catalog.
type AssetLister interface {
	ListAllAssets(ctx context.Context, alias string) ([]catalog.Asset, error)
}

// LibrarySource yields the configured libraries to walk. mediapath.Resolver
// satisfies it via Libraries().
type LibrarySource interface {
	Libraries() []catalog.Library
}

// BackfillResult summarizes a backfill pass.
type BackfillResult struct {
	Indexed int // assets successfully (re)indexed
	Failed  int // assets whose extraction/index errored (logged, not fatal)
}

// Backfill re-extracts and indexes metadata for every catalogued asset across
// all libraries. It is the explicit/bulk counterpart to ingest-time indexing,
// driven by the scan / warm-cache CLI paths so an existing library gets indexed
// without waiting for files to change. A per-asset failure is logged and counted
// but never aborts the pass (one unreadable file must not block the rest).
func (idx *Indexer) Backfill(ctx context.Context, libs LibrarySource, lister AssetLister, log *slog.Logger) (BackfillResult, error) {
	if log == nil {
		log = slog.Default()
	}
	var res BackfillResult
	for _, lib := range libs.Libraries() {
		assets, err := lister.ListAllAssets(ctx, lib.Alias)
		if err != nil {
			return res, fmt.Errorf("search: backfill list %s: %w", lib.Alias, err)
		}
		for _, a := range assets {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			if err := idx.IndexAsset(ctx, a); err != nil {
				res.Failed++
				log.Warn("search: backfill index asset", "asset", a.ID, "err", err)
				continue
			}
			res.Indexed++
		}
	}
	return res, nil
}
