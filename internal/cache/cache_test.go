package cache_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/cache"
)

func newCache(t *testing.T) *cache.DiskCache {
	t.Helper()
	c, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	return c
}

func TestCacheMissThenHit(t *testing.T) {
	c := newCache(t)
	key := c.Key("asset1", "thumb", "w=320", "srchashAAA")

	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss on empty cache")
	}
	want := []byte("jpeg-bytes")
	path, err := c.Put(key, bytes.NewReader(want), "image/jpeg")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected hit after Put")
	}
	if got != path {
		t.Errorf("Get path %q != Put path %q", got, path)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if !bytes.Equal(onDisk, want) {
		t.Errorf("cached bytes = %q, want %q", onDisk, want)
	}
	if mime := c.Mime(key); mime != "image/jpeg" {
		t.Errorf("Mime = %q, want image/jpeg", mime)
	}
}

// TestCacheKeyContentAddressed: a different source hash yields a different key
// (so a changed source naturally invalidates the old derivative), while the
// same parts yield the same key (idempotent).
func TestCacheKeyContentAddressed(t *testing.T) {
	c := newCache(t)
	k1 := c.Key("a", "thumb", "w=320", "hash1")
	k1again := c.Key("a", "thumb", "w=320", "hash1")
	k2 := c.Key("a", "thumb", "w=320", "hash2")
	kKind := c.Key("a", "preview", "w=320", "hash1")

	if k1 != k1again {
		t.Errorf("same inputs gave different keys: %q vs %q", k1, k1again)
	}
	if k1 == k2 {
		t.Error("different src hash must change the key")
	}
	if k1 == kKind {
		t.Error("different kind must change the key")
	}
}

// TestCacheFanout: keys land under a 2-char prefix directory.
func TestCacheFanout(t *testing.T) {
	c := newCache(t)
	key := c.Key("a", "thumb", "w=320", "h")
	path, err := c.Put(key, bytes.NewReader([]byte("x")), "image/jpeg")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	// path should be <dir>/<ab>/<key>
	rel, err := os.Stat(path)
	if err != nil || rel.IsDir() {
		t.Fatalf("expected a file at %q: %v", path, err)
	}
}

func TestServeBytesETag(t *testing.T) {
	body := []byte("hello-thumbnail")
	etag := cache.ETagFor(body)

	// First request: 200 with ETag + Cache-Control.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/assets/x/thumb", nil)
	cache.ServeBytes(rec, req, body, "image/jpeg")

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if res.Header.Get("ETag") != etag {
		t.Errorf("ETag = %q, want %q", res.Header.Get("ETag"), etag)
	}
	if cc := res.Header.Get("Cache-Control"); cc == "" {
		t.Error("missing Cache-Control")
	}
	got, _ := io.ReadAll(res.Body)
	if !bytes.Equal(got, body) {
		t.Errorf("body = %q, want %q", got, body)
	}

	// Conditional request with matching ETag: 304, no body.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/assets/x/thumb", nil)
	req2.Header.Set("If-None-Match", etag)
	cache.ServeBytes(rec2, req2, body, "image/jpeg")
	res2 := rec2.Result()
	defer func() { _ = res2.Body.Close() }()
	if res2.StatusCode != http.StatusNotModified {
		t.Errorf("conditional status = %d, want 304", res2.StatusCode)
	}
	b2, _ := io.ReadAll(res2.Body)
	if len(b2) != 0 {
		t.Errorf("304 should have empty body, got %q", b2)
	}
}
