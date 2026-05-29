package main

import (
	"bytes"
	"context"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/disintegration/imaging"

	mediacache "github.com/imyousuf/fs-image-manager/internal/cache"
	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/internaltest"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
	"github.com/imyousuf/fs-image-manager/internal/search"
	"github.com/imyousuf/fs-image-manager/internal/server"
)

// TestServeWiresJobResultHandlers is the task #16 regression: it proves the
// serve-time wiring (buildJobAPI, used by registerInternalJobAPI) actually
// registers the cache result-handlers, so a derivative a worker POSTs to
// /jobs/{id}/result lands in the real derivative cache AND is recorded as a
// catalog.Derivative. The original bug built jobs.NewRegistry() inline and
// discarded it, silently dropping every transform result.
func TestServeWiresJobResultHandlers(t *testing.T) {
	ctx := context.Background()

	// Real migrated DB shared by the queue and the catalog repo (one file, as in
	// production).
	conn, err := db.Open(ctx, filepath.Join(t.TempDir(), "fsim.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	root := t.TempDir()
	resolver, err := mediapath.NewResolver([]catalog.Library{{Alias: "pictures", Name: "Pictures", Root: root}})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}

	// The catalog asset must exist so the derivative row's FK to assets holds.
	repo := catalog.NewRepo(conn, media.ClassifyExt)
	asset := catalog.Group([]catalog.FileInput{{
		Alias: "pictures", Dir: "shoot", Name: "IMG.CR3",
		MediaPath: "pictures/shoot/IMG.CR3",
	}}, media.ClassifyExt)[0]
	if err := repo.PutAsset(ctx, asset); err != nil {
		t.Fatalf("PutAsset: %v", err)
	}

	dc, err := mediacache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache: %v", err)
	}

	// Enqueue a develop-raw job (a transform kind the cache handlers cover).
	queue := newJobQueue(conn)
	job, err := queue.Enqueue(ctx, catalog.JobKindDevelopRAW, asset.ID, asset.DisplayPath, nil)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Build the internal job API exactly as serve does, mount on a plain mux
	// (the API's RegisterRoutes uses no /internal/jobs prefix or auth — those are
	// the server's job, out of scope for this wiring test).
	mux := http.NewServeMux()
	buildJobAPI(queue, resolver, dc, repo, newLogger()).RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	// POST a derivative result for the job (multipart, as the worker does).
	derivBytes := []byte("developed-jpeg-bytes-from-worker")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("kind", "developed-jpg")
	_ = mw.WriteField("mime", "image/jpeg")
	part, _ := mw.CreateFormFile("file", "out.jpg")
	_, _ = part.Write(derivBytes)
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/"+job.ID+"/result", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST result: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("result status = %d, want 200: %s", res.StatusCode, b)
	}

	// 1) The derivative must be recorded in the catalog, pointing at a cache path.
	deriv, err := repo.GetDerivative(ctx, asset.ID, "developed-jpg", "")
	if err != nil {
		t.Fatalf("derivative not recorded in catalog (result was dropped): %v", err)
	}
	if deriv.Mime != "image/jpeg" || deriv.Path == "" {
		t.Fatalf("derivative row malformed: %+v", deriv)
	}

	// 2) The bytes must be in the cache at that path.
	onDisk, err := os.ReadFile(deriv.Path)
	if err != nil {
		t.Fatalf("derivative bytes not in cache at %q: %v", deriv.Path, err)
	}
	if !bytes.Equal(onDisk, derivBytes) {
		t.Errorf("cached derivative bytes = %q, want %q", onDisk, derivBytes)
	}
}

// TestBuildMediaStackWiresSearchIndexing is the task #17 regression: it proves
// buildMediaStack assembles the search indexer AND wires it into the ingester,
// so a file that arrives via reconcile is metadata-indexed and findable through
// the search index — without main.go doing any extra wiring. It also exercises
// the assetViewer adapter that renders search hits as media.AssetViews.
func TestBuildMediaStackWiresSearchIndexing(t *testing.T) {
	ctx := context.Background()

	cfg, roots := internaltest.NewConfig(t)
	resolver, err := mediapath.NewResolver(cfg.Libraries())
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	conn, err := db.Open(ctx, cfg.DBPath())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Assemble the full stack exactly as the CLI commands do (nil queue = no
	// worker). This is what wires search.NewIndexer into ingest.Options.Indexer.
	stack, err := buildMediaStack(cfg, conn, resolver, nil, newLogger())
	if err != nil {
		t.Fatalf("buildMediaStack: %v", err)
	}
	if stack.index == nil || stack.indexer == nil {
		t.Fatal("stack did not assemble the search index/indexer")
	}

	// Drop a real JPEG into the pictures library and reconcile. Ingest upserts
	// the asset and (via the wired indexer) indexes its metadata + filename.
	writeTestJPEG(t, filepath.Join(roots.Pictures, "trip", "Sunset.JPG"))
	if _, err := stack.ingester.ReconcileAll(ctx); err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}

	// The asset must be findable by its filename through the search index — this
	// only works if ingest actually called the indexer (the #17 wiring).
	res, err := stack.index.Search(ctx, search.Query{Text: "Sunset", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.AssetIDs) != 1 {
		t.Fatalf("search for 'Sunset' returned %d hits, want 1 (ingest did not index)", len(res.AssetIDs))
	}

	// The assetViewer adapter renders the hit as a media.AssetView.
	views, err := (assetViewer{repo: stack.repo}).ViewsForIDs(ctx, res.AssetIDs)
	if err != nil {
		t.Fatalf("ViewsForIDs: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("viewer returned %d views, want 1", len(views))
	}
	if av, ok := views[0].(media.AssetView); !ok || av.Name != "Sunset" {
		t.Errorf("view = %#v, want a media.AssetView named Sunset", views[0])
	}
}

// writeTestJPEG writes a small solid-colour JPEG at abs (creating parents).
func writeTestJPEG(t *testing.T, abs string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		t.Fatalf("create %s: %v", abs, err)
	}
	defer func() { _ = f.Close() }()
	img := imaging.New(64, 48, color.NRGBA{R: 220, G: 120, B: 40, A: 255})
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

// TestWireServeLifecycle exercises the single serve-time assembly entrypoint:
// wireServe must register the media + search routes on the server, start the
// ingest watcher, and return a stop func that shuts the watcher down cleanly and
// idempotently. We assert a public route is mounted (libraries) and that
// stop() returns promptly and is safe to call twice.
func TestWireServeLifecycle(t *testing.T) {
	cfg, _ := internaltest.NewConfig(t)
	resolver, err := mediapath.NewResolver(cfg.Libraries())
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	conn, err := db.Open(context.Background(), cfg.DBPath())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	srv := server.New(server.Options{
		Addr:         ":0",
		WorkerSecret: "s",
		Resolver:     resolver,
		Logger:       newLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := wireServe(ctx, cfg, conn, resolver, srv, newLogger())
	if err != nil {
		t.Fatalf("wireServe: %v", err)
	}

	// All feature routes must be registered on the API mux — media, search, AND
	// ai-people (the seam built in ahead of ai-people landing). srv.API() is the
	// pre-strip mux, so request paths without the /api prefix. A registered route
	// returns a non-404; an unregistered pattern 404s. We assert each is mounted.
	for _, route := range []struct {
		name, method, path string
	}{
		{"media libraries", http.MethodGet, "/libraries"},
		{"search", http.MethodGet, "/assets/search"},
		{"people list", http.MethodGet, "/people"},
	} {
		rec := httptest.NewRecorder()
		srv.API().ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s route %q not registered (404)", route.name, route.path)
		}
	}

	// stop() must return promptly (watcher shuts down) and be idempotent.
	doneCh := make(chan struct{})
	go func() { stop(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("stop() did not return within 5s (watcher leak)")
	}
	stop() // second call is a no-op via sync.Once; must not panic or block
}
