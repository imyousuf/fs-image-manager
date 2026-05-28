package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/db/store"
)

// This file is media-pipeline-owned: the catalog repository over the sqlc
// store. It persists and loads assets/files/derivatives and serves the browse
// queries. The type *shapes* (Asset/File/Derivative) are platform-owned in
// types.go; the behaviour here is ours (docs/specs/_contracts.md §1).

// ErrNotFound is returned when a requested asset/derivative does not exist.
var ErrNotFound = errors.New("catalog: not found")

// timeLayout is the storage format for file mod-times and captured-at.
const timeLayout = time.RFC3339Nano

// fileEntry is the grouping logic's view of one on-disk file: enough to bucket
// and to build a File. It is unexported; the repo and ingest construct it.
type fileEntry struct {
	alias     string
	dir       string
	name      string // base filename with extension
	kind      FileKind
	mediaPath MediaPath
	size      int64
	modTime   time.Time
	hash      string
}

// Classifier maps a filename to its FileKind. The repo and ingest are wired
// with media.ClassifyExt; tests may pass their own.
type Classifier func(name string) FileKind

// Repo is the catalog repository. It is safe for concurrent use to the extent
// the underlying *sql.DB is (the app caps SQLite to a single writer).
type Repo struct {
	q        *store.Queries
	classify Classifier
}

// NewRepo builds a Repo over db using classify to determine file kinds.
func NewRepo(db store.DBTX, classify Classifier) *Repo {
	return &Repo{q: store.New(db), classify: classify}
}

// --- persistence -----------------------------------------------------------

// PutAsset upserts an asset and all its files in one logical operation. Files
// not in a.Files for this asset are NOT removed here (use ReplaceAssetFiles for
// that); PutAsset is additive/idempotent for the upsert path.
func (r *Repo) PutAsset(ctx context.Context, a Asset) error {
	if err := r.q.UpsertAsset(ctx, store.UpsertAssetParams{
		ID:           a.ID,
		Alias:        a.Alias,
		Dir:          a.Dir,
		BaseName:     a.BaseName,
		Kind:         a.Kind,
		DisplayPath:  string(a.DisplayPath),
		NeedsDevelop: boolToInt(NeedsDevelop(a)),
	}); err != nil {
		return fmt.Errorf("catalog: upsert asset %s: %w", a.ID, err)
	}
	for _, f := range a.Files {
		if err := r.putFile(ctx, a.ID, f); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) putFile(ctx context.Context, assetID string, f File) error {
	if err := r.q.UpsertFile(ctx, store.UpsertFileParams{
		MediaPath: string(f.MediaPath),
		AssetID:   assetID,
		Kind:      string(f.Kind),
		Size:      f.Size,
		ModTime:   f.ModTime.UTC().Format(timeLayout),
		Hash:      f.Hash,
	}); err != nil {
		return fmt.Errorf("catalog: upsert file %s: %w", f.MediaPath, err)
	}
	return nil
}

// DeleteFile removes a single file row. If its asset has no remaining files the
// asset (and its derivatives, via cascade) is pruned too. It reports whether
// the owning asset was pruned and the asset id, so callers can reap cache
// derivatives for a removed asset.
func (r *Repo) DeleteFile(ctx context.Context, mp MediaPath) (assetPruned bool, assetID string, err error) {
	// Discover the owning asset before deleting so we can check emptiness after.
	row, err := r.q.GetFile(ctx, string(mp))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", nil // already gone
		}
		return false, "", fmt.Errorf("catalog: lookup file %s: %w", mp, err)
	}
	owner := row.AssetID
	if err := r.q.DeleteFile(ctx, string(mp)); err != nil {
		return false, "", fmt.Errorf("catalog: delete file %s: %w", mp, err)
	}
	n, err := r.q.CountFilesForAsset(ctx, owner)
	if err != nil {
		return false, "", fmt.Errorf("catalog: count files for %s: %w", owner, err)
	}
	if n == 0 {
		if err := r.q.DeleteAsset(ctx, owner); err != nil {
			return false, "", fmt.Errorf("catalog: delete empty asset %s: %w", owner, err)
		}
		return true, owner, nil
	}
	return false, owner, nil
}

// DeleteAsset removes an asset and (via FK cascade) its files and derivative
// rows. Returns ErrNotFound semantics softly: deleting a missing asset is a
// no-op.
func (r *Repo) DeleteAsset(ctx context.Context, id string) error {
	if err := r.q.DeleteAsset(ctx, id); err != nil {
		return fmt.Errorf("catalog: delete asset %s: %w", id, err)
	}
	return nil
}

// --- reads ------------------------------------------------------------------

// GetAsset loads an asset and its files by id.
func (r *Repo) GetAsset(ctx context.Context, id string) (Asset, error) {
	row, err := r.q.GetAsset(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Asset{}, fmt.Errorf("%w: asset %s", ErrNotFound, id)
		}
		return Asset{}, fmt.Errorf("catalog: get asset %s: %w", id, err)
	}
	files, err := r.filesFor(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	return assetFromRow(row, files), nil
}

func (r *Repo) filesFor(ctx context.Context, assetID string) ([]File, error) {
	rows, err := r.q.ListFilesForAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("catalog: list files for %s: %w", assetID, err)
	}
	out := make([]File, 0, len(rows))
	for _, fr := range rows {
		out = append(out, fileFromRow(fr))
	}
	return out, nil
}

// Page is a page of assets plus the cursor to fetch the next page ("" = last).
type Page struct {
	Assets     []Asset
	NextCursor string
	// Dirs holds the immediate child folder names of the listed directory, so
	// the browse view can render subfolders alongside assets.
	Dirs []string
}

// DefaultPageLimit bounds a browse page when the caller does not specify one.
const DefaultPageLimit = 200

// maxPageLimit caps an over-eager caller.
const maxPageLimit = 1000

// ListByDir returns one page of assets directly in (alias, dir), ordered by
// base name, using keyset pagination: cursor is the last base name from the
// previous page ("" to start). It also returns the immediate child directories
// of dir so the UI can show subfolders. A nil/zero limit uses DefaultPageLimit.
func (r *Repo) ListByDir(ctx context.Context, alias, dir, cursor string, limit int) (Page, error) {
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	rows, err := r.q.ListAssetsByDir(ctx, store.ListAssetsByDirParams{
		Alias:    alias,
		Dir:      dir,
		BaseName: cursor,
		Limit:    int64(limit) + 1, // fetch one extra to detect a next page
	})
	if err != nil {
		return Page{}, fmt.Errorf("catalog: list assets %s/%s: %w", alias, dir, err)
	}

	var next string
	if len(rows) > limit {
		next = rows[limit-1].BaseName
		rows = rows[:limit]
	}

	assets := make([]Asset, 0, len(rows))
	for _, row := range rows {
		files, ferr := r.filesFor(ctx, row.ID)
		if ferr != nil {
			return Page{}, ferr
		}
		assets = append(assets, assetFromRow(row, files))
	}

	dirs, err := r.childDirs(ctx, alias, dir)
	if err != nil {
		return Page{}, err
	}
	return Page{Assets: assets, NextCursor: next, Dirs: dirs}, nil
}

// childDirs computes the immediate child folder names of (alias, dir) from the
// set of all asset directories in the library. SQLite-side substring math was
// brittle through sqlc, so the derivation lives here in Go.
func (r *Repo) childDirs(ctx context.Context, alias, dir string) ([]string, error) {
	all, err := r.q.ListDirsForLibrary(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("catalog: list dirs %s: %w", alias, err)
	}
	seen := make(map[string]struct{})
	var prefix string
	if dir != "" {
		prefix = dir + "/"
	}
	for _, d := range all {
		var rest string
		switch {
		case dir == "":
			rest = d
		case d == dir:
			continue // the dir itself, not a child
		case strings.HasPrefix(d, prefix):
			rest = d[len(prefix):]
		default:
			continue
		}
		if rest == "" {
			continue
		}
		// First path segment of rest is the immediate child.
		child := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			child = rest[:i]
		}
		seen[child] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// ListAllAssets returns every asset in a library (with files), in folder order.
// Used by warm-cache and the reconcile-driven bulk passes.
func (r *Repo) ListAllAssets(ctx context.Context, alias string) ([]Asset, error) {
	rows, err := r.q.ListAllAssets(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("catalog: list all assets %s: %w", alias, err)
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		files, ferr := r.filesFor(ctx, row.ID)
		if ferr != nil {
			return nil, ferr
		}
		out = append(out, assetFromRow(row, files))
	}
	return out, nil
}

// CatalogFile pairs a media path with the (mtime,size,hash) the catalog has on
// record, for the reconcile diff.
type CatalogFile struct {
	MediaPath MediaPath
	Kind      FileKind
	Size      int64
	ModTime   time.Time
	Hash      string
	AssetID   string
}

// SnapshotFiles returns every catalogued file in a library keyed by media path,
// for the reconcile scan to diff against the on-disk walk on (mtime,size).
func (r *Repo) SnapshotFiles(ctx context.Context, alias string) (map[MediaPath]CatalogFile, error) {
	rows, err := r.q.ListAllFiles(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("catalog: snapshot files %s: %w", alias, err)
	}
	out := make(map[MediaPath]CatalogFile, len(rows))
	for _, fr := range rows {
		mt, _ := time.Parse(timeLayout, fr.ModTime)
		mp := MediaPath(fr.MediaPath)
		out[mp] = CatalogFile{
			MediaPath: mp,
			Kind:      FileKind(fr.Kind),
			Size:      fr.Size,
			ModTime:   mt,
			Hash:      fr.Hash,
			AssetID:   fr.AssetID,
		}
	}
	return out, nil
}

// SetCapturedAt records an asset's capture time (search-index calls this).
func (r *Repo) SetCapturedAt(ctx context.Context, id string, t time.Time) error {
	s := t.UTC().Format(timeLayout)
	if err := r.q.SetAssetCapturedAt(ctx, store.SetAssetCapturedAtParams{CapturedAt: &s, ID: id}); err != nil {
		return fmt.Errorf("catalog: set captured_at %s: %w", id, err)
	}
	return nil
}

// --- derivatives ------------------------------------------------------------

// PutDerivative records a cached derivative for an asset.
func (r *Repo) PutDerivative(ctx context.Context, d Derivative, srcHash string) error {
	if err := r.q.UpsertDerivative(ctx, store.UpsertDerivativeParams{
		AssetID: d.AssetID,
		Kind:    d.Kind,
		Params:  d.Params,
		Path:    d.Path,
		Mime:    d.Mime,
		SrcHash: srcHash,
	}); err != nil {
		return fmt.Errorf("catalog: upsert derivative %s/%s: %w", d.AssetID, d.Kind, err)
	}
	return nil
}

// GetDerivative loads a derivative by (assetID, kind, params), or ErrNotFound.
func (r *Repo) GetDerivative(ctx context.Context, assetID, kind, params string) (Derivative, error) {
	row, err := r.q.GetDerivative(ctx, store.GetDerivativeParams{AssetID: assetID, Kind: kind, Params: params})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Derivative{}, fmt.Errorf("%w: derivative %s/%s", ErrNotFound, assetID, kind)
		}
		return Derivative{}, fmt.Errorf("catalog: get derivative %s/%s: %w", assetID, kind, err)
	}
	return Derivative{
		AssetID: row.AssetID,
		Kind:    row.Kind,
		Params:  row.Params,
		Path:    row.Path,
		Mime:    row.Mime,
	}, nil
}

// --- row<->domain mapping ---------------------------------------------------

func assetFromRow(row store.Asset, files []File) Asset {
	a := Asset{
		ID:          row.ID,
		Alias:       row.Alias,
		Dir:         row.Dir,
		BaseName:    row.BaseName,
		Kind:        row.Kind,
		Files:       files,
		DisplayPath: MediaPath(row.DisplayPath),
	}
	if row.CapturedAt != nil {
		if t, err := time.Parse(timeLayout, *row.CapturedAt); err == nil {
			a.CapturedAt = &t
		}
	}
	return a
}

func fileFromRow(fr store.File) File {
	mt, _ := time.Parse(timeLayout, fr.ModTime)
	return File{
		MediaPath: MediaPath(fr.MediaPath),
		Kind:      FileKind(fr.Kind),
		Size:      fr.Size,
		ModTime:   mt,
		Hash:      fr.Hash,
	}
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
