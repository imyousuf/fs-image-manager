package internaltest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/internaltest"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

func TestNewConfigProducesUsableResolver(t *testing.T) {
	cfg, roots := internaltest.NewConfig(t)
	libs := cfg.Libraries()
	if len(libs) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(libs))
	}
	if libs[0].Root != roots.Pictures || libs[1].Root != roots.Videos {
		t.Errorf("roots mismatch: %+v vs %+v", libs, roots)
	}
	// The fixture config should drive a working resolver.
	r, err := mediapath.NewResolver(libs)
	if err != nil {
		t.Fatalf("NewResolver from fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(roots.Pictures, "x.jpg"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := r.Open("pictures/x.jpg")
	if err != nil {
		t.Fatalf("Open via fixture resolver: %v", err)
	}
	_ = f.Close()
	// Low debounce for tests.
	if cfg.IngestDebounce().Seconds() != 1 {
		t.Errorf("fixture debounce = %v, want 1s", cfg.IngestDebounce())
	}
	if cfg.WorkerSecret() != "test-secret" {
		t.Errorf("fixture worker secret = %q", cfg.WorkerSecret())
	}
}

func TestCopySampleMediaMissingSrc(t *testing.T) {
	// A nonexistent source returns nil (callers t.Skip on empty).
	got := internaltest.CopySampleMedia(t, filepath.Join(t.TempDir(), "nope"), t.TempDir(), 3)
	if got != nil {
		t.Errorf("expected nil for missing src, got %v", got)
	}
}

func TestCopySampleMediaCopies(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	for _, name := range []string{"a.jpg", "b.MP4", "notes.txt", "c.cr3"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := internaltest.CopySampleMedia(t, src, dst, 10)
	// notes.txt is not media; the other three are.
	if len(got) != 3 {
		t.Fatalf("copied %v, want 3 media files", got)
	}
	for _, base := range got {
		if strings.HasSuffix(base, ".txt") {
			t.Errorf("non-media copied: %q", base)
		}
		if _, err := os.Stat(filepath.Join(dst, base)); err != nil {
			t.Errorf("expected copied file %q: %v", base, err)
		}
	}
}

func TestFakeCache(t *testing.T) {
	c := internaltest.NewFakeCache()
	key := c.Key("asset1", "thumb", "w=320", "hash")
	if _, ok := c.Get(key); ok {
		t.Fatal("empty cache reported hit")
	}
	if _, err := c.Put(key, strings.NewReader("bytes"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(key); !ok {
		t.Fatal("expected hit after Put")
	}
	data, mime, ok := c.Bytes(key)
	if !ok || string(data) != "bytes" || mime != "image/jpeg" {
		t.Errorf("Bytes = %q %q %v", data, mime, ok)
	}
}

func TestFakeQueue(t *testing.T) {
	ctx := context.Background()
	q := internaltest.NewFakeQueue()
	job, err := q.Enqueue(ctx, "transcode-video", "asset1", "videos/c.mp4", map[string]string{"crf": "20"})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "pending" {
		t.Errorf("status = %q", job.Status)
	}
	if len(q.Pending()) != 1 {
		t.Fatalf("pending = %d", len(q.Pending()))
	}
	claimed, err := q.Claim(ctx, []string{"transcode-video"}, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claimed = %+v", claimed)
	}
	if len(q.Pending()) != 0 {
		t.Error("claimed job still pending")
	}
	if err := q.Complete(ctx, job.ID, catalog.JobResult{DerivativeKind: "mp4", Mime: "video/mp4"}); err != nil {
		t.Fatal(err)
	}
	// Claim with a non-matching kind returns nothing.
	more, _ := q.Claim(ctx, []string{"develop-raw"}, 0, 5)
	if len(more) != 0 {
		t.Errorf("unexpected claim across kinds: %+v", more)
	}
}

func TestFakeEnricher(t *testing.T) {
	e := &internaltest.FakeEnricher{Result: catalog.EnrichResult{Labels: []string{"cat"}, Caption: "a cat"}}
	res, err := e.Enrich(context.Background(), "pictures/cat.jpg", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Caption != "a cat" || len(res.Labels) != 1 {
		t.Errorf("result = %+v", res)
	}
	if calls := e.Calls(); len(calls) != 1 || calls[0] != "pictures/cat.jpg" {
		t.Errorf("calls = %v", calls)
	}
}

func TestFakeFaceRecognizer(t *testing.T) {
	f := &internaltest.FakeFaceRecognizer{PersonID: "p1", Confidence: 0.98}
	id, conf, err := f.Match(context.Background(), catalog.Face{ID: "f1", AssetID: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "p1" || conf != 0.98 {
		t.Errorf("match = %q %v", id, conf)
	}
}
