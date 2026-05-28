package ingest

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/media"
)

// regroupDir re-lists every stable, classified file currently in (alias, dir),
// regroups them into assets, and upserts. It must consider ALL siblings — not
// only the changed file — so a newly arrived JPG joins its existing RAW's asset
// (and flips that asset off needs-develop). Grouping reads the directory afresh.
func (in *Ingester) regroupDir(ctx context.Context, lib catalog.Library, dir string, res *ScanResult) error {
	inputs, err := in.listDir(lib, dir)
	if err != nil {
		return err
	}
	assets := catalog.Group(inputs, media.ClassifyExt)
	for _, a := range assets {
		// Hash member files (cheap head+size) so derivatives can be keyed on
		// content and reconcile can detect changes. We hash here, once, at
		// ingest time rather than per request.
		in.hashAsset(&a)
		if err := in.opts.Repo.PutAsset(ctx, a); err != nil {
			return err
		}
		res.Assets++
		res.Enqueued += in.enqueueForAsset(ctx, a)
		in.indexAsset(ctx, a)
	}
	return nil
}

// indexAsset invokes the optional metadata Indexer (search-index) for a freshly
// upserted asset. A nil Indexer is a no-op; an index failure is logged but never
// aborts the reconcile pass (browsing/thumbnails must not depend on indexing).
func (in *Ingester) indexAsset(ctx context.Context, a catalog.Asset) {
	if in.opts.Indexer == nil {
		return
	}
	if err := in.opts.Indexer.IndexAsset(ctx, a); err != nil {
		in.log.Warn("ingest: index asset", "asset", a.ID, "err", err)
	}
}

// listDir reads (alias, dir) from disk and builds catalog.FileInput for each
// regular, non-hidden file. It does not recurse: grouping is per directory.
func (in *Ingester) listDir(lib catalog.Library, dir string) ([]catalog.FileInput, error) {
	mpDir := lib.Alias
	if dir != "" {
		mpDir += "/" + dir
	}
	f, err := in.opts.Resolver.Open(catalog.MediaPath(mpDir))
	if err != nil {
		// Directory vanished between walk and re-list; nothing to group.
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	var out []catalog.FileInput
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}
		rel := name
		if dir != "" {
			rel = dir + "/" + name
		}
		abs := filepath.Join(lib.Root, filepath.FromSlash(rel))
		info, serr := os.Stat(abs)
		if serr != nil || info.IsDir() {
			continue
		}
		out = append(out, catalog.FileInput{
			Alias:     lib.Alias,
			Dir:       dir,
			Name:      name,
			MediaPath: catalog.MediaPath(lib.Alias + "/" + rel),
			Size:      info.Size(),
			ModTime:   info.ModTime(),
		})
	}
	return out, nil
}

// hashAsset fills the content hash of each member file (head+size). Errors are
// non-fatal — a file that cannot be hashed is left with an empty hash and will
// be retried on the next reconcile.
func (in *Ingester) hashAsset(a *catalog.Asset) {
	for i := range a.Files {
		abs, _, _, err := in.opts.Resolver.Resolve(a.Files[i].MediaPath)
		if err != nil {
			continue
		}
		if h, herr := media.HashFile(abs); herr == nil {
			a.Files[i].Hash = h
		}
	}
}

// enqueueForAsset enqueues the appropriate derivative/enrich jobs for a freshly
// upserted asset. A nil Queue (no worker) is a no-op — host thumbnails/posters
// still serve on demand. Returns the number of jobs enqueued.
//
// Job policy (docs/specs/media-pipeline.md §1,§4; face-index per ai-people §9.2):
//   - image with a host display source (JPG/PNG) -> convert-image (WebP/AVIF)
//     plus enrich-ai plus face-index. Thumbs/previews are host-cheap and
//     generated on demand, not queued.
//   - RAW-only image (needs develop) -> develop-raw, then enrich-ai + face-index
//     (those run once the develop job produces the display JPG; the jobs are
//     content-hash cached so a premature run with no display source is harmless).
//   - video -> transcode-video + enrich-ai. The poster is host-cheap (ffmpeg
//     single frame) and produced on demand. Face detection on video is out of
//     scope (DSLR photo library is the target) — no face-index for videos.
func (in *Ingester) enqueueForAsset(ctx context.Context, a catalog.Asset) int {
	if in.opts.Queue == nil {
		return 0
	}
	var n int
	enqueue := func(kind string, params map[string]string) {
		if _, err := in.opts.Queue.Enqueue(ctx, kind, a.ID, a.DisplayPath, params); err != nil {
			in.log.Warn("ingest: enqueue job", "kind", kind, "asset", a.ID, "err", err)
			return
		}
		n++
	}

	switch {
	case a.Kind == catalog.AssetKindVideo:
		enqueue(catalog.JobKindTranscodeVideo, nil)
		enqueue(catalog.JobKindEnrichAI, nil)
	case catalog.NeedsDevelop(a):
		// RAW-only: develop a JPG derivative first; the develop job carries the
		// RAW path explicitly since DisplayPath is empty.
		raw := rawMember(a)
		if _, err := in.opts.Queue.Enqueue(ctx, catalog.JobKindDevelopRAW, a.ID, raw, nil); err == nil {
			n++
		}
		enqueue(catalog.JobKindEnrichAI, nil)
		enqueue(catalog.JobKindFaceIndex, nil)
	case a.Kind == catalog.AssetKindImage:
		enqueue(catalog.JobKindConvertImage, nil)
		enqueue(catalog.JobKindEnrichAI, nil)
		enqueue(catalog.JobKindFaceIndex, nil)
	}
	return n
}

// rawMember returns the media path of the asset's RAW member, if any.
func rawMember(a catalog.Asset) catalog.MediaPath {
	for _, f := range a.Files {
		if f.Kind == catalog.FileKindRAW {
			return f.MediaPath
		}
	}
	return a.DisplayPath
}

// isStable re-stats abs after the stability window and reports whether its
// (mtime,size) held steady — i.e. the file is not still being written. An
// unstable file is deferred to a later pass.
func (in *Ingester) isStable(ctx context.Context, abs string) (bool, error) {
	first, err := os.Stat(abs)
	if err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(in.opts.StabilityWindow):
	}
	second, err := os.Stat(abs)
	if err != nil {
		return false, err
	}
	stable := first.Size() == second.Size() && first.ModTime().Equal(second.ModTime())
	return stable, nil
}

// reapDerivatives removes cached derivative files for a pruned asset. The
// catalog rows are already gone via FK cascade; this best-effort step keeps the
// cache from accumulating orphans. Cache reaping by content hash is the
// authoritative GC; this is the eager path. Currently a hook for the cache —
// kept deliberately conservative (no-op) to avoid deleting bytes still
// referenceable by another asset sharing the same source hash.
func (in *Ingester) reapDerivatives(assetID string) {
	in.log.Debug("ingest: asset pruned", "asset", assetID)
}

// isHiddenDir reports whether a directory name should be skipped during the
// walk (dotfiles and common non-media system dirs).
func isHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".")
}

// dirOf returns the library-relative directory of a relative path ("" = root).
func dirOf(rel string) string {
	d := path.Dir(rel)
	if d == "." {
		return ""
	}
	return d
}
