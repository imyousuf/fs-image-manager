package media

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// Warmer pre-generates derivatives for an entire library. It backs the
// `warm-cache` CLI command (docs/specs/media-pipeline.md §6): generate all
// thumbnails/posters, idempotent by source content hash (a cache hit is
// skipped), and enqueue develop-raw / enrich jobs where a worker is configured.
type Warmer struct {
	resolver mediapath.Resolver
	repo     *catalog.Repo
	cache    catalog.Cache
	gen      *Handlers // reuse the same generate+key logic the request path uses
	log      *slog.Logger
}

// NewWarmer builds a Warmer. queue may be nil (no worker).
func NewWarmer(resolver mediapath.Resolver, repo *catalog.Repo, cache catalog.Cache, log *slog.Logger) *Warmer {
	if log == nil {
		log = slog.Default()
	}
	return &Warmer{
		resolver: resolver,
		repo:     repo,
		cache:    cache,
		gen:      NewHandlers(HandlerOptions{Resolver: resolver, Repo: repo, Cache: cache}),
		log:      log,
	}
}

// WarmResult summarises a warm-cache pass.
type WarmResult struct {
	Assets    int
	Generated int // derivatives freshly generated
	Hits      int // derivatives already cached (skipped)
}

// WarmAll warms every library; see Warm.
func (wm *Warmer) WarmAll(ctx context.Context) (WarmResult, error) {
	var total WarmResult
	for _, lib := range wm.resolver.Libraries() {
		res, err := wm.Warm(ctx, lib.Alias)
		if err != nil {
			return total, err
		}
		total.Assets += res.Assets
		total.Generated += res.Generated
		total.Hits += res.Hits
	}
	return total, nil
}

// Warm pre-generates the thumbnail and preview for every asset in a library,
// skipping those already cached (idempotent by source hash). Generation failures
// are logged and skipped — warming is best-effort and never aborts the run.
func (wm *Warmer) Warm(ctx context.Context, alias string) (WarmResult, error) {
	var res WarmResult
	assets, err := wm.repo.ListAllAssets(ctx, alias)
	if err != nil {
		return res, err
	}
	for _, a := range assets {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.Assets++
		for _, spec := range []struct {
			kind string
			size int
		}{
			{"thumb", DefaultThumbSize},
			{"preview", DefaultPreviewSize},
		} {
			if wm.warmOne(ctx, a, spec.kind, spec.size) {
				res.Generated++
			} else {
				res.Hits++
			}
		}
	}
	return res, nil
}

// warmOne ensures a single derivative exists in the cache, generating it on a
// miss. It returns true if a fresh derivative was generated, false on a cache
// hit (or on a generation failure, which is logged).
func (wm *Warmer) warmOne(ctx context.Context, a catalog.Asset, kind string, size int) bool {
	srcHash := displaySourceHash(a)
	params := "w=" + strconv.Itoa(size)
	key := wm.cache.Key(a.ID, kind, params, srcHash)
	if _, ok := wm.cache.Get(key); ok {
		return false // already warm
	}
	data, mime, err := wm.gen.generate(ctx, a, kind, size)
	if err != nil {
		wm.log.Warn("warm-cache: generate", "asset", a.ID, "kind", kind, "err", err)
		return false
	}
	if _, err := wm.cache.Put(key, bytes.NewReader(data), mime); err != nil {
		wm.log.Warn("warm-cache: cache put", "asset", a.ID, "kind", kind, "err", err)
		return false
	}
	_ = wm.repo.PutDerivative(ctx, catalog.Derivative{
		AssetID: a.ID, Kind: kind, Params: params, Mime: mime,
	}, srcHash)
	return true
}
