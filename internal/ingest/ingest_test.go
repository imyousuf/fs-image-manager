package ingest_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/ingest"
	"github.com/imyousuf/fs-image-manager/internal/internaltest"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

type fixture struct {
	roots    internaltest.Roots
	resolver mediapath.Resolver
	repo     *catalog.Repo
	queue    *internaltest.FakeQueue
}

func newFixture(t *testing.T, debounce, stability time.Duration) (*ingest.Ingester, *fixture) {
	t.Helper()
	roots := internaltest.NewTempRoots(t)
	resolver, err := mediapath.NewResolver([]catalog.Library{
		{Alias: "pictures", Name: "Pictures", Root: roots.Pictures},
		{Alias: "videos", Name: "Videos", Root: roots.Videos},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "fsim.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	repo := catalog.NewRepo(conn, media.ClassifyExt)
	queue := internaltest.NewFakeQueue()

	ing, err := ingest.New(ingest.Options{
		Resolver:        resolver,
		Repo:            repo,
		Queue:           queue,
		Debounce:        debounce,
		StabilityWindow: stability,
	})
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}
	return ing, &fixture{roots: roots, resolver: resolver, repo: repo, queue: queue}
}

func writeFile(t *testing.T, root, rel string, data []byte) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// TestReconcileAddModDelete exercises the full diff: an add is catalogued, a
// modification is re-catalogued, and a deletion prunes the asset.
func TestReconcileAddModDelete(t *testing.T) {
	ing, fx := newFixture(t, time.Second, 5*time.Millisecond)
	ctx := context.Background()

	writeFile(t, fx.roots.Pictures, "2021/IMG.JPG", []byte("first-version-bytes"))
	res, err := ing.ReconcileAll(ctx)
	if err != nil {
		t.Fatalf("reconcile add: %v", err)
	}
	if res.Added != 1 || res.Assets < 1 {
		t.Fatalf("add pass = %+v, want Added=1", res)
	}
	page, _ := fx.repo.ListByDir(ctx, "pictures", "2021", "", 0)
	if len(page.Assets) != 1 {
		t.Fatalf("expected 1 asset after add, got %d", len(page.Assets))
	}

	// Modify: change content+size; mtime usually changes too.
	time.Sleep(10 * time.Millisecond)
	writeFile(t, fx.roots.Pictures, "2021/IMG.JPG", []byte("second-version-much-longer-bytes"))
	res, err = ing.ReconcileAll(ctx)
	if err != nil {
		t.Fatalf("reconcile mod: %v", err)
	}
	if res.Modified != 1 {
		t.Errorf("mod pass = %+v, want Modified=1", res)
	}

	// Delete.
	if err := os.Remove(filepath.Join(fx.roots.Pictures, "2021/IMG.JPG")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	res, err = ing.ReconcileAll(ctx)
	if err != nil {
		t.Fatalf("reconcile delete: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("delete pass = %+v, want Deleted=1", res)
	}
	page, _ = fx.repo.ListByDir(ctx, "pictures", "2021", "", 0)
	if len(page.Assets) != 0 {
		t.Errorf("asset not pruned after delete: %d remain", len(page.Assets))
	}
}

// TestReconcileGroupsRawJpg: a RAW + JPG sharing a basename in a watched root
// become ONE asset with the JPG as display, end-to-end through reconcile.
func TestReconcileGroupsRawJpg(t *testing.T) {
	ing, fx := newFixture(t, time.Second, 5*time.Millisecond)
	ctx := context.Background()

	writeFile(t, fx.roots.Pictures, "shoot/F1.CR3", []byte("raw-bytes-pretend"))
	writeFile(t, fx.roots.Pictures, "shoot/F1.JPG", []byte("jpg-bytes-pretend"))
	writeFile(t, fx.roots.Pictures, "shoot/F1.xmp", []byte("<xmp/>"))
	if _, err := ing.ReconcileAll(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	page, _ := fx.repo.ListByDir(ctx, "pictures", "shoot", "", 0)
	if len(page.Assets) != 1 {
		t.Fatalf("RAW+JPG+XMP should be 1 asset, got %d", len(page.Assets))
	}
	a := page.Assets[0]
	if a.DisplayPath != "pictures/shoot/F1.JPG" {
		t.Errorf("DisplayPath = %q, want the JPG", a.DisplayPath)
	}
	if len(a.Files) != 3 {
		t.Errorf("members = %d, want 3", len(a.Files))
	}
}

// TestReconcileMultiRoot: both libraries are scanned.
func TestReconcileMultiRoot(t *testing.T) {
	ing, fx := newFixture(t, time.Second, 5*time.Millisecond)
	ctx := context.Background()
	writeFile(t, fx.roots.Pictures, "p.JPG", []byte("pic"))
	writeFile(t, fx.roots.Videos, "v.webm", []byte("vid-bytes"))
	if _, err := ing.ReconcileAll(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	pPage, _ := fx.repo.ListByDir(ctx, "pictures", "", "", 0)
	vPage, _ := fx.repo.ListByDir(ctx, "videos", "", "", 0)
	if len(pPage.Assets) != 1 || len(vPage.Assets) != 1 {
		t.Errorf("multi-root: pictures=%d videos=%d, want 1 each", len(pPage.Assets), len(vPage.Assets))
	}
	if vPage.Assets[0].Kind != catalog.AssetKindVideo {
		t.Errorf("video asset kind = %q", vPage.Assets[0].Kind)
	}
}

// TestReconcileEnqueuesJobs: an image enqueues convert+enrich+face-index; a
// RAW-only enqueues develop+enrich+face-index; a video enqueues
// transcode+enrich but NO face-index (face detection on video is out of scope).
func TestReconcileEnqueuesJobs(t *testing.T) {
	ing, fx := newFixture(t, time.Second, 5*time.Millisecond)
	ctx := context.Background()
	writeFile(t, fx.roots.Pictures, "img/photo.JPG", []byte("jpg-image"))
	writeFile(t, fx.roots.Pictures, "raw/only.CR3", []byte("raw-only"))
	writeFile(t, fx.roots.Videos, "clip.mp4", []byte("video"))
	if _, err := ing.ReconcileAll(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	kinds := map[string]int{}
	for _, j := range fx.queue.Pending() {
		kinds[j.Kind]++
	}
	if kinds[catalog.JobKindConvertImage] == 0 {
		t.Error("JPG image should enqueue convert-image")
	}
	if kinds[catalog.JobKindDevelopRAW] == 0 {
		t.Error("RAW-only should enqueue develop-raw")
	}
	if kinds[catalog.JobKindTranscodeVideo] == 0 {
		t.Error("video should enqueue transcode-video")
	}
	if kinds[catalog.JobKindEnrichAI] < 3 {
		t.Errorf("all three assets should enqueue enrich-ai, got %d", kinds[catalog.JobKindEnrichAI])
	}
	// face-index: exactly the two image assets (JPG + RAW-only), never the video.
	if kinds[catalog.JobKindFaceIndex] != 2 {
		t.Errorf("face-index should be enqueued for the 2 image assets only, got %d", kinds[catalog.JobKindFaceIndex])
	}
}

// fakeIndexer records the assets handed to it, for the indexer-hook test.
type fakeIndexer struct {
	mu      sync.Mutex
	indexed []catalog.Asset
}

func (f *fakeIndexer) IndexAsset(_ context.Context, a catalog.Asset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.indexed = append(f.indexed, a)
	return nil
}

func (f *fakeIndexer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.indexed)
}

// TestReconcileCallsIndexer: a configured Indexer (search-index seam) is invoked
// for each added/changed asset, receiving the upserted Asset with its members.
func TestReconcileCallsIndexer(t *testing.T) {
	roots := internaltest.NewTempRoots(t)
	resolver, err := mediapath.NewResolver([]catalog.Library{
		{Alias: "pictures", Name: "Pictures", Root: roots.Pictures},
		{Alias: "videos", Name: "Videos", Root: roots.Videos},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "fsim.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	repo := catalog.NewRepo(conn, media.ClassifyExt)
	idx := &fakeIndexer{}

	ing, err := ingest.New(ingest.Options{
		Resolver:        resolver,
		Repo:            repo,
		Indexer:         idx,
		Debounce:        time.Second,
		StabilityWindow: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ingest.New: %v", err)
	}

	writeFile(t, roots.Pictures, "a.JPG", []byte("img-a"))
	writeFile(t, roots.Pictures, "b.JPG", []byte("img-b"))
	if _, err := ing.ReconcileAll(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if idx.count() != 2 {
		t.Errorf("indexer called %d times, want 2", idx.count())
	}
	for _, a := range idx.indexed {
		if len(a.Files) == 0 || a.DisplayPath == "" {
			t.Errorf("indexer received an incomplete asset: %+v", a)
		}
	}
}

// TestReconcileNilIndexerOK: with no Indexer configured, ingestion still works
// (browsing/cataloging must not depend on search-index).
func TestReconcileNilIndexerOK(t *testing.T) {
	ing, fx := newFixture(t, time.Second, 5*time.Millisecond) // newFixture sets no Indexer
	writeFile(t, fx.roots.Pictures, "solo.JPG", []byte("img"))
	if _, err := ing.ReconcileAll(context.Background()); err != nil {
		t.Fatalf("reconcile with nil indexer: %v", err)
	}
	page, _ := fx.repo.ListByDir(context.Background(), "pictures", "", "", 0)
	if len(page.Assets) != 1 {
		t.Errorf("nil indexer should not block cataloging, got %d assets", len(page.Assets))
	}
}

// TestStabilitySkipsGrowingFile: a file whose size changes across the stability
// window is deferred (not catalogued) this pass.
func TestStabilitySkipsGrowingFile(t *testing.T) {
	// Long stability window so the test can mutate the file mid-check.
	ing, fx := newFixture(t, time.Second, 150*time.Millisecond)
	ctx := context.Background()

	abs := filepath.Join(fx.roots.Pictures, "growing.JPG")
	writeFile(t, fx.roots.Pictures, "growing.JPG", []byte("start"))

	// Grow the file shortly after reconcile begins its stability wait.
	go func() {
		time.Sleep(40 * time.Millisecond)
		f, err := os.OpenFile(abs, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		_, _ = f.WriteString("more-and-more-bytes-appended")
		_ = f.Close()
	}()

	res, err := ing.ReconcileAll(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Skipped < 1 {
		t.Errorf("growing file should be skipped, res=%+v", res)
	}
	page, _ := fx.repo.ListByDir(ctx, "pictures", "", "", 0)
	if len(page.Assets) != 0 {
		t.Errorf("unstable file should not be catalogued yet, got %d assets", len(page.Assets))
	}

	// A subsequent pass (file now stable) catalogs it.
	res2, err := ing.ReconcileAll(ctx)
	if err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	if res2.Added != 1 {
		t.Errorf("second pass should catalog the now-stable file, res=%+v", res2)
	}
}
