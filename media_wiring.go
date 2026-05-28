package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	mediacache "github.com/imyousuf/fs-image-manager/internal/cache"
	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/config"
	"github.com/imyousuf/fs-image-manager/internal/enrich"
	"github.com/imyousuf/fs-image-manager/internal/ingest"
	"github.com/imyousuf/fs-image-manager/internal/jobs"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
	"github.com/imyousuf/fs-image-manager/internal/metadata"
	"github.com/imyousuf/fs-image-manager/internal/people"
	"github.com/imyousuf/fs-image-manager/internal/search"
	"github.com/imyousuf/fs-image-manager/internal/server"
)

// mediaStack bundles the media-pipeline + search components the CLI commands
// wire up: the catalog repo, the derivative cache, the ingester, and the
// search index/indexer. It is assembled by buildMediaStack from the already-open
// db connection and resolver. The job Queue is wired by serve only (the worker
// is a separate process); jobs queue via the durable jobs.Queue when present.
type mediaStack struct {
	repo     *catalog.Repo
	cache    *mediacache.DiskCache
	ingester *ingest.Ingester
	// index/indexer are the search-index components. index backs the search
	// HTTP handlers; indexer is driven by ingest (post-upsert) and by the
	// scan/warm-cache Backfill. The indexer is also injected into the ingester
	// as its catalog.Indexer so EXIF/ffprobe extraction runs during ingestion.
	index   *search.Index
	indexer *search.Indexer
}

// buildMediaStack constructs the catalog repo, derivative cache, search index +
// indexer, and the ingester (with the indexer wired in as its post-upsert
// metadata hook) from a loaded config, an open db connection and a resolver.
// queue may be nil (no worker configured) — browsing/thumbnails still work.
//
// The search indexer is built here, inside the assembly, rather than threaded
// in by the callers: that keeps main.go thin and means every entry point
// (serve/scan/warm-cache) gets consistent ingest-time indexing for free. A
// metadata extractor with no ffprobe still indexes images and filenames.
func buildMediaStack(cfg *config.Config, conn *sql.DB, resolver mediapath.Resolver, queue catalog.Queue, log *slog.Logger) (*mediaStack, error) {
	repo := catalog.NewRepo(conn, media.ClassifyExt)

	dc, err := mediacache.New(cfg.CacheDir())
	if err != nil {
		return nil, fmt.Errorf("build cache: %w", err)
	}

	// Search index + indexer (search-index). The FTS5/metadata tables are
	// created by db.Open's goose migrations (0200 range); search.New only needs
	// the open connection. repo satisfies search.CaptureSetter so capture times
	// flow back onto the catalog asset for the browse view.
	ix := search.New(conn)
	extractor := metadata.NewExtractor(metadata.Options{Resolver: resolver})
	indexer := search.NewIndexer(ix, extractor, repo)

	ing, err := ingest.New(ingest.Options{
		Resolver: resolver,
		Repo:     repo,
		Queue:    queue,
		Indexer:  indexer, // post-upsert metadata extraction (nil-guarded in ingest)
		Debounce: cfg.IngestDebounce(),
		Logger:   log,
	})
	if err != nil {
		return nil, fmt.Errorf("build ingester: %w", err)
	}

	return &mediaStack{repo: repo, cache: dc, ingester: ing, index: ix, indexer: indexer}, nil
}

// wireServe is the SINGLE serve-time assembly entrypoint. cmdServe calls it once
// and is otherwise oblivious to how the media/jobs/search wiring evolves: this
// function owns the whole graph — durable job queue, media stack (catalog repo,
// derivative cache, ingester with the search indexer wired in), the worker-
// facing internal job API with the #16 cache-result-handlers, the public media
// routes (browse/thumb/preview/stream/download/upload), and the search +
// timeline routes — and it starts the hands-off ingestion watcher.
//
// It returns a stop func the caller defers: stop cancels the watcher and blocks
// until the goroutine has exited, so shutdown is clean and leak-free. ctx is the
// process context (cancelled on signal); the watcher also stops when ctx is
// cancelled, making stop idempotent and safe to call after ctx is already done.
//
// Keeping this here (not in main.go) means platform's main.go never changes as
// the registry/indexer/route wiring evolves — the seam stays in the package that
// owns it.
func wireServe(ctx context.Context, cfg *config.Config, conn *sql.DB, resolver mediapath.Resolver, srv *server.Server, log *slog.Logger) (stop func(), err error) {
	// Durable job queue. Producers (ingest, search, ai-people) enqueue via this
	// catalog.Queue; the worker claims over the internal API. With no worker,
	// jobs simply queue — browsing/thumbnails never block on it.
	jobQueue := newJobQueue(conn)

	// Media + search assembly (catalog repo, cache, ingester+indexer, search index).
	stack, err := buildMediaStack(cfg, conn, resolver, jobQueue, log)
	if err != nil {
		return nil, err
	}

	// People repo (ai-people) over the shared connection — drives the public
	// People API, the enrich-ai run ledger, and the serve-side face-index
	// assignment. Serve stays CGO/AWS-free: the face-index job is split — the
	// worker runs Recognize (AWS/GPU), the serve host runs Assign (DB-only, nil
	// recognizer), so no AWS dependency is pulled into serve.
	peopleRepo := people.NewRepo(conn)
	viewer := assetViewer{repo: stack.repo}

	// Worker-facing internal job API, wired AFTER the stack. Its result-handler
	// registry persists worker output into the same cache/catalog/search the
	// public API serves from:
	//   - #16 cache handlers for transcode-video/develop-raw/convert-image,
	//   - enrich-ai → AI labels/caption/embedding into search + run ledger,
	//   - face-index → assign detected faces to Person clusters (DB-only Assign;
	//     nil recognizer is safe, the serve side never recognizes). Without this
	//     last registration, face-index results POSTed by the worker are silently
	//     dropped — same class as the #16 bug, for faces.
	// No-op routing without a worker secret.
	reg := jobs.NewRegistry()
	jobs.RegisterCacheHandlers(reg, stack.cache, stack.repo)
	reg.Register(catalog.JobKindEnrichAI, enrich.ResultHandler(stack.index, peopleRepo))
	facePipeline := people.NewPipeline(nil, peopleRepo, stack.index, cfg.RekognitionCollection())
	people.RegisterResultHandler(reg, facePipeline)
	jobs.NewAPI(jobQueue, resolver, reg, log).RegisterRoutes(srv.Internal())

	// Public media routes; upload triggers a synchronous reconcile of its library
	// so the new asset appears immediately.
	media.NewHandlers(media.HandlerOptions{
		Resolver: resolver,
		Repo:     stack.repo,
		Cache:    stack.cache,
		OnUpload: func(c context.Context, alias string) {
			stack.reconcileAlias(c, resolver, alias, log)
		},
	}).Register(srv.API())

	// Public search + timeline routes. The viewer renders hit ids into the same
	// AssetView shape folder-browse emits (media.ToView), so the two list
	// endpoints are identical on the wire.
	search.NewHandlers(stack.index, viewer).Register(srv.API())

	// Public People routes (ai-people): GET /people, /people/{id}/assets, rename.
	// Same viewer → "photos of X" emits identical AssetViews to browse/search.
	people.NewHandlers(peopleRepo, viewer).Register(srv.API())

	// Start the hands-off ingestion watcher under a child context so stop() can
	// shut it down independently of (and ahead of) the HTTP server.
	watchCtx, cancelWatch := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if werr := stack.ingester.Watch(watchCtx); werr != nil {
			log.Error("ingest watcher stopped", "err", werr)
		}
	}()

	var stopOnce sync.Once
	stop = func() {
		stopOnce.Do(func() {
			cancelWatch()
			<-done
		})
	}
	return stop, nil
}

// newJobQueue builds the durable, SQLite-backed job queue (jobs-worker's
// internal/jobs, which implements catalog.Queue) over the shared db connection.
// It is isolated here so the cross-package wiring is reviewable in one place.
func newJobQueue(conn *sql.DB) *jobs.Queue {
	return jobs.NewQueue(conn)
}

// buildJobAPI constructs the internal job API with a result-handler registry
// that has the media-transform cache handlers installed (transcode/develop/
// convert → cache + catalog.Derivative; the bug fixed in task #16 was a
// discarded registry). wireServe builds the production registry inline so it can
// also add the enrich-ai handler; this helper is the cache-only assembly the
// isolated #16 regression test exercises without the server's handler chain.
func buildJobAPI(queue *jobs.Queue, resolver mediapath.Resolver, cache catalog.Cache, deriv jobs.DerivativeWriter, log *slog.Logger) *jobs.API {
	reg := jobs.NewRegistry()
	jobs.RegisterCacheHandlers(reg, cache, deriv)
	return jobs.NewAPI(queue, resolver, reg, log)
}

// reconcileAlias runs a reconcile of a single library by alias; used as the
// upload-trigger hook so an uploaded file appears in listings immediately.
func (m *mediaStack) reconcileAlias(ctx context.Context, resolver mediapath.Resolver, alias string, log *slog.Logger) {
	for _, lib := range resolver.Libraries() {
		if lib.Alias != alias {
			continue
		}
		if _, err := m.ingester.Reconcile(ctx, lib); err != nil {
			log.Warn("upload: reconcile after upload", "alias", alias, "err", err)
		}
		return
	}
}

// assetViewer adapts the catalog repo + media's view builder to search's
// AssetViewer port: it loads each asset by id and renders it as a media.AssetView
// (boxed into any so search need not depend on internal/media). Order in == order
// out; ids that no longer resolve are dropped, matching the port's contract.
// wireServe constructs it for both search.NewHandlers and people.NewHandlers,
// so search hits and "photos of X" emit byte-identical AssetViews to folder
// browse. Order in == order out.
type assetViewer struct {
	repo *catalog.Repo
}

// ViewsForIDs loads each asset by id and renders it as a media.AssetView, boxed
// into any so the search/people packages need not import internal/media. A hit
// whose asset was pruned between index and query (catalog.ErrNotFound) is
// skipped so a stale index row never fails the whole response; any other error
// (e.g. a real DB failure) is surfaced.
func (v assetViewer) ViewsForIDs(ctx context.Context, ids []string) ([]any, error) {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		a, err := v.repo.GetAsset(ctx, id)
		if err != nil {
			if errors.Is(err, catalog.ErrNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, media.ToView(a))
	}
	return out, nil
}
