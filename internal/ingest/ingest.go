// Package ingest is the hands-off ingestion pipeline: an fsnotify watcher with
// a trailing debounce, a periodic reconcile scan that diffs the on-disk tree
// against the catalog, and a per-file stability check so partially-copied files
// are never processed mid-write. Detected adds/mods upsert assets and enqueue
// thumb/poster (+ develop-raw / enrich) jobs; deletes prune the catalog and
// derivatives. See docs/TECH_SPEC.md §7.2 and docs/specs/media-pipeline.md §4.
//
// The pipeline degrades gracefully: a nil job Queue means "no worker
// configured" — browsing and host thumbnails still work; heavy jobs are simply
// not enqueued.
package ingest

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// Options configures an Ingester.
// Indexer is the metadata-extraction seam (implemented by search-index's
// internal/metadata). After an asset is added or changed, the ingester calls
// IndexAsset so EXIF/ffprobe extraction and FTS indexing run as part of
// ingestion. It is OPTIONAL and nil-guarded exactly like Queue: with no indexer
// configured, browsing/thumbnails still work and nothing is indexed.
//
// The contract is deliberately narrow — it hands over the upserted Asset (which
// carries its member files + DisplayPath) and lets the implementor decide how
// to extract and persist metadata (e.g. via catalog.Repo.SetCapturedAt and its
// own search tables). Errors are logged by the ingester, never fatal: a failed
// index must not abort a reconcile pass.
type Indexer interface {
	IndexAsset(ctx context.Context, a catalog.Asset) error
}

type Options struct {
	Resolver mediapath.Resolver
	Repo     *catalog.Repo
	// Queue is the heavy-job queue. nil = no worker; jobs are not enqueued.
	Queue catalog.Queue
	// Indexer runs metadata extraction/indexing for added/changed assets
	// (search-index). nil = no indexing; ingestion still catalogs + caches.
	Indexer Indexer
	// Debounce is the trailing quiet period after the last fs event before a
	// watcher-triggered scan runs. From [ingest] debounce_seconds.
	Debounce time.Duration
	// StabilityWindow is how long a file's (mtime,size) must hold steady before
	// it is considered safe to process. Defaults to a fraction of Debounce.
	StabilityWindow time.Duration
	// Logger; defaults to slog.Default().
	Logger *slog.Logger
	// now is injectable for tests; defaults to time.Now.
	now func() time.Time
}

// Ingester runs reconcile scans and (optionally) a live watcher over the
// configured libraries.
type Ingester struct {
	opts Options
	log  *slog.Logger
	now  func() time.Time
}

// New builds an Ingester. Repo and Resolver are required; Queue may be nil.
func New(opts Options) (*Ingester, error) {
	if opts.Resolver == nil {
		return nil, fmt.Errorf("ingest: nil resolver")
	}
	if opts.Repo == nil {
		return nil, fmt.Errorf("ingest: nil repo")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.StabilityWindow <= 0 {
		// A short, bounded default; for tiny test debounces this stays small.
		opts.StabilityWindow = minDuration(opts.Debounce/4, 2*time.Second)
		if opts.StabilityWindow <= 0 {
			opts.StabilityWindow = 50 * time.Millisecond
		}
	}
	return &Ingester{opts: opts, log: opts.Logger, now: opts.now}, nil
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// ScanResult summarises a reconcile pass over one or more libraries.
type ScanResult struct {
	Added    int
	Modified int
	Deleted  int
	Skipped  int // files deferred by the stability check
	Assets   int // assets touched (upserted)
	Enqueued int // jobs enqueued
}

func (s *ScanResult) add(o ScanResult) {
	s.Added += o.Added
	s.Modified += o.Modified
	s.Deleted += o.Deleted
	s.Skipped += o.Skipped
	s.Assets += o.Assets
	s.Enqueued += o.Enqueued
}

// ReconcileAll runs a reconcile scan over every configured library.
func (in *Ingester) ReconcileAll(ctx context.Context) (ScanResult, error) {
	var total ScanResult
	for _, lib := range in.opts.Resolver.Libraries() {
		res, err := in.Reconcile(ctx, lib)
		if err != nil {
			return total, err
		}
		total.add(res)
	}
	return total, nil
}

// Reconcile walks one library's tree, diffs it against the catalog on
// (mtime,size) and applies the differences: new/changed files are grouped and
// upserted (enqueuing jobs); files gone from disk are pruned. Unstable files
// (still being written) are skipped this pass and picked up by the next.
func (in *Ingester) Reconcile(ctx context.Context, lib catalog.Library) (ScanResult, error) {
	var res ScanResult

	snapshot, err := in.opts.Repo.SnapshotFiles(ctx, lib.Alias)
	if err != nil {
		return res, err
	}

	// touchedDirs is the set of directories holding an add/mod this pass; we
	// re-group each one wholesale afterwards (grouping is per (alias,dir,base),
	// and a re-group must see all siblings, not just the changed file).
	touchedDirs := map[string]struct{}{}
	seen := map[catalog.MediaPath]struct{}{}

	walkErr := filepath.WalkDir(lib.Root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries; reconcile is best-effort
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if isHiddenDir(d.Name()) && abs != lib.Root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(lib.Root, abs)
		if rerr != nil {
			return nil //nolint:nilerr // unexpected; skip
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(path.Base(rel), ".") {
			return nil // skip dotfiles
		}
		mp := catalog.MediaPath(lib.Alias + "/" + rel)

		info, ierr := d.Info()
		if ierr != nil {
			return nil //nolint:nilerr // vanished between walk and stat
		}
		seen[mp] = struct{}{}

		cat, known := snapshot[mp]
		changed := !known || cat.Size != info.Size() || !cat.ModTime.Equal(info.ModTime().UTC().Truncate(time.Nanosecond))
		if !changed {
			return nil // unchanged; nothing to do
		}

		// Stability check: re-stat after the window; defer if still changing.
		stable, serr := in.isStable(ctx, abs)
		if serr != nil || !stable {
			res.Skipped++
			return nil
		}

		touchedDirs[dirOf(rel)] = struct{}{}
		if known {
			res.Modified++
		} else {
			res.Added++
		}
		return nil
	})
	if walkErr != nil {
		return res, fmt.Errorf("ingest: walk %s: %w", lib.Root, walkErr)
	}

	// Prune files that vanished from disk.
	for mp := range snapshot {
		if _, ok := seen[mp]; ok {
			continue
		}
		pruned, assetID, derr := in.opts.Repo.DeleteFile(ctx, mp)
		if derr != nil {
			return res, derr
		}
		res.Deleted++
		if pruned {
			in.reapDerivatives(assetID)
		}
	}

	// Re-group and upsert each touched directory. We re-list the *entire*
	// directory's stable files (not just the changed ones) so grouping sees all
	// siblings — a newly arrived JPG must join its existing RAW's asset.
	for dir := range touchedDirs {
		if err := in.regroupDir(ctx, lib, dir, &res); err != nil {
			return res, err
		}
	}

	return res, nil
}
