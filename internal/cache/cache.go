// Package cache implements catalog.Cache: a disk-backed, content-addressed store
// for derivative bytes (thumbnails, posters, previews, …). Keys fold in the
// source content hash so a changed source produces a different key and the old
// derivative is simply never read again. Layout, per the media-pipeline spec:
//
//	<cacheDir>/<ab>/<key>
//
// where <ab> is the first two hex chars of the key's digest (a 256-way fan-out
// so a single directory never holds the whole library's derivatives). Serving
// helpers add ETag/Cache-Control so browsers and the SPA cache aggressively.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// compile-time assertion that DiskCache satisfies the shared port.
var _ catalog.Cache = (*DiskCache)(nil)

// DiskCache is a content-addressed derivative cache rooted at Dir.
type DiskCache struct {
	dir string
}

// New creates a DiskCache rooted at dir, creating the directory if needed.
func New(dir string) (*DiskCache, error) {
	if dir == "" {
		return nil, fmt.Errorf("cache: empty cache dir")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("cache: resolve dir %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("cache: mkdir %q: %w", abs, err)
	}
	return &DiskCache{dir: abs}, nil
}

// Dir returns the cache root directory.
func (c *DiskCache) Dir() string { return c.dir }

// Key composes a content-addressed cache key from its parts. The key embeds the
// source hash so a changed source yields a new key (natural invalidation). The
// human-readable prefix (assetID-kind-params) aids debugging; the trailing
// digest guarantees filesystem-safety and uniqueness.
func (c *DiskCache) Key(assetID, kind, params, srcHash string) string {
	raw := strings.Join([]string{assetID, kind, params, srcHash}, "|")
	sum := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(sum[:])
	// Keep a short, sanitised readable hint plus the full digest.
	hint := sanitize(kind)
	if params != "" {
		hint += "-" + sanitize(params)
	}
	return digest[:32] + "-" + hint
}

// path maps a key to its on-disk location with a 2-char fan-out prefix.
func (c *DiskCache) path(key string) string {
	prefix := "00"
	if len(key) >= 2 {
		prefix = key[:2]
	}
	return filepath.Join(c.dir, prefix, key)
}

// Get returns the cached file path for key and whether it exists on disk.
func (c *DiskCache) Get(key string) (string, bool) {
	p := c.path(key)
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p, true
	}
	return "", false
}

// Put stores r under key with the given mime and returns its path. Writes go to
// a temp file then rename so a reader never sees a half-written derivative.
// The mime is recorded in a sidecar so serving can set Content-Type without
// re-sniffing; absence falls back to extension/sniff at serve time.
func (c *DiskCache) Put(key string, r io.Reader, mime string) (string, error) {
	final := c.path(key)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", fmt.Errorf("cache: mkdir for %q: %w", key, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("cache: temp for %q: %w", key, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup if we error out before the rename.
		_ = os.Remove(tmpName)
	}()
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("cache: write %q: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("cache: close temp %q: %w", key, err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		return "", fmt.Errorf("cache: commit %q: %w", key, err)
	}
	if mime != "" {
		// Sidecar is advisory; ignore write failure (serving can sniff).
		_ = os.WriteFile(final+".mime", []byte(mime), 0o644)
	}
	return final, nil
}

// Mime returns the recorded mime type for a cached key (from the sidecar), or
// "" if unknown.
func (c *DiskCache) Mime(key string) string {
	b, err := os.ReadFile(c.path(key) + ".mime")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// sanitize keeps a string filesystem- and key-safe: lower-case alnum plus '=',
// everything else collapses to '_'. Bounded length keeps keys tidy.
func sanitize(s string) string {
	const maxLen = 24
	var b strings.Builder
	for i, r := range s {
		if i >= maxLen {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '=':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ServeBytes writes b to w as a cacheable response with an ETag derived from the
// content and the given mime. It honours conditional requests (If-None-Match)
// via http.ServeContent semantics for the simple in-memory case.
func ServeBytes(w http.ResponseWriter, r *http.Request, b []byte, mime string) {
	etag := ETagFor(b)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if mime != "" {
		w.Header().Set("Content-Type", mime)
	}
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(b)))
	_, _ = w.Write(b)
}

// ETagFor returns a strong ETag (quoted hex sha256 prefix) for b.
func ETagFor(b []byte) string {
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
