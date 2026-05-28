package media_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"

	"github.com/imyousuf/fs-image-manager/internal/cache"
	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/internaltest"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// harness wires a resolver, migrated repo, disk cache and the media handlers
// over two temp library roots, returning an httptest server.
type harness struct {
	roots    internaltest.Roots
	resolver mediapath.Resolver
	repo     *catalog.Repo
	srv      *httptest.Server
}

func newHarness(t *testing.T) *harness {
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
	dc, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatalf("cache: %v", err)
	}

	h := &harness{roots: roots, resolver: resolver, repo: repo}
	mux := http.NewServeMux()
	media.NewHandlers(media.HandlerOptions{
		Resolver: resolver,
		Repo:     repo,
		Cache:    dc,
		OnUpload: func(_ context.Context, _ string) {}, // reconcile triggered manually in tests
	}).Register(mux)
	h.srv = httptest.NewServer(mux)
	t.Cleanup(h.srv.Close)
	return h
}

// writeJPEG writes a solid w x h JPEG into the pictures root at rel and
// catalogs it via the repo (grouping a single image).
func (h *harness) writeJPEG(t *testing.T, rel string, w, hgt int) catalog.Asset {
	t.Helper()
	abs := filepath.Join(h.roots.Pictures, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	img := imaging.New(w, hgt, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		_ = f.Close()
		t.Fatalf("encode: %v", err)
	}
	_ = f.Close()

	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		dir = ""
	}
	name := filepath.Base(rel)
	assets := catalog.Group([]catalog.FileInput{{
		Alias: "pictures", Dir: dir, Name: name,
		MediaPath: catalog.MediaPath("pictures/" + rel),
	}}, media.ClassifyExt)
	if err := h.repo.PutAsset(context.Background(), assets[0]); err != nil {
		t.Fatalf("PutAsset: %v", err)
	}
	return assets[0]
}

func (h *harness) get(t *testing.T, path string) *http.Response {
	t.Helper()
	res, err := http.Get(h.srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return res
}

func TestHandlerListLibraries(t *testing.T) {
	h := newHarness(t)
	res := h.get(t, "/libraries")
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var libs []media.LibraryView
	if err := json.NewDecoder(res.Body).Decode(&libs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(libs) != 2 || libs[0].Alias != "pictures" || libs[1].Alias != "videos" {
		t.Errorf("libraries = %+v", libs)
	}
}

func TestHandlerBrowsePagination(t *testing.T) {
	h := newHarness(t)
	for _, n := range []string{"A.JPG", "B.JPG", "C.JPG"} {
		h.writeJPEG(t, "2021/"+n, 64, 64)
	}
	res := h.get(t, "/assets?path=pictures/2021&limit=2")
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var page struct {
		Items []media.AssetView `json:"items"`
		Next  string            `json:"next"`
	}
	if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 2 || page.Next == "" {
		t.Fatalf("page = %d items, next=%q; want 2 + cursor", len(page.Items), page.Next)
	}
	if page.Items[0].DisplayThumb == "" || page.Items[0].Stream == "" {
		t.Error("asset view missing derivative URLs")
	}
}

func TestHandlerBrowseTraversalReject(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []string{
		"/assets?path=pictures/../videos",
		"/assets?path=pictures/../../etc",
		"/assets?path=/etc/passwd",
		"/assets?path=unknownalias/x",
	} {
		res := h.get(t, bad)
		if res.StatusCode == 200 {
			t.Errorf("traversal/invalid path %q returned 200", bad)
		}
		_ = res.Body.Close()
	}
}

func TestHandlerThumbAndETag(t *testing.T) {
	h := newHarness(t)
	a := h.writeJPEG(t, "t/Big.JPG", 800, 400)

	res := h.get(t, "/assets/"+a.ID+"/thumb?size=200")
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		t.Fatalf("thumb status %d", res.StatusCode)
	}
	if res.Header.Get("ETag") == "" {
		t.Error("thumb missing ETag")
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) == 0 {
		t.Fatal("empty thumb")
	}
	// Second request with If-None-Match -> 304.
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/assets/"+a.ID+"/thumb?size=200", nil)
	req.Header.Set("If-None-Match", res.Header.Get("ETag"))
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	defer func() { _ = res2.Body.Close() }()
	if res2.StatusCode != http.StatusNotModified {
		t.Errorf("conditional thumb status = %d, want 304", res2.StatusCode)
	}
}

func TestHandlerStreamRange(t *testing.T) {
	h := newHarness(t)
	a := h.writeJPEG(t, "r/Orig.JPG", 200, 200)

	// Full GET to learn the size.
	full := h.get(t, "/assets/"+a.ID+"/stream")
	fullBody, _ := io.ReadAll(full.Body)
	_ = full.Body.Close()
	if full.Header.Get("Accept-Ranges") != "bytes" {
		t.Error("stream should advertise Accept-Ranges: bytes")
	}
	if len(fullBody) < 10 {
		t.Fatalf("stream body too small: %d", len(fullBody))
	}

	// Ranged GET: first 10 bytes.
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/assets/"+a.ID+"/stream", nil)
	req.Header.Set("Range", "bytes=0-9")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ranged GET: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", res.StatusCode)
	}
	part, _ := io.ReadAll(res.Body)
	if len(part) != 10 {
		t.Errorf("range body = %d bytes, want 10", len(part))
	}
	if !bytes.Equal(part, fullBody[:10]) {
		t.Error("ranged bytes do not match the original prefix")
	}
}

func TestHandlerDownloadZip(t *testing.T) {
	h := newHarness(t)
	h.writeJPEG(t, "z/One.JPG", 32, 32)
	h.writeJPEG(t, "z/Two.JPG", 32, 32)

	body, _ := json.Marshal(map[string][]string{
		"paths": {"pictures/z/One.JPG", "pictures/z/Two.JPG"},
	})
	res, err := http.Post(h.srv.URL+"/download", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /download: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		t.Fatalf("zip status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	zipBytes, _ := io.ReadAll(res.Body)
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["pictures/z/One.JPG"] || !names["pictures/z/Two.JPG"] {
		t.Errorf("zip members = %v, want both alias-prefixed paths", names)
	}
}

func TestHandlerDownloadZipTraversalReject(t *testing.T) {
	h := newHarness(t)
	body, _ := json.Marshal(map[string][]string{"paths": {"pictures/../../etc/passwd"}})
	res, err := http.Post(h.srv.URL+"/download", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == 200 {
		t.Error("traversal path in zip request returned 200")
	}
}

func TestHandlerUploadAppearsInListing(t *testing.T) {
	h := newHarness(t)

	// Build a multipart body with one JPEG.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", "Uploaded.JPG")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	img := imaging.New(48, 48, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	if err := jpeg.Encode(part, img, nil); err != nil {
		t.Fatalf("encode upload: %v", err)
	}
	_ = mw.Close()

	res, err := http.Post(h.srv.URL+"/upload?alias=pictures&dir=incoming", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST /upload: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("upload status = %d: %s", res.StatusCode, b)
	}

	// The file is on disk under the library; the upload hook is a no-op in the
	// harness, so drive the catalog explicitly by re-grouping the dir the same
	// way ingest would, then assert it lists.
	abs := filepath.Join(h.roots.Pictures, "incoming", "Uploaded.JPG")
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("uploaded file not on disk: %v", err)
	}
	asset := catalog.Group([]catalog.FileInput{{
		Alias: "pictures", Dir: "incoming", Name: "Uploaded.JPG",
		MediaPath: "pictures/incoming/Uploaded.JPG",
	}}, media.ClassifyExt)
	if err := h.repo.PutAsset(context.Background(), asset[0]); err != nil {
		t.Fatalf("PutAsset: %v", err)
	}

	listRes := h.get(t, "/assets?path=pictures/incoming")
	defer func() { _ = listRes.Body.Close() }()
	var page struct {
		Items []media.AssetView `json:"items"`
	}
	_ = json.NewDecoder(listRes.Body).Decode(&page)
	if len(page.Items) != 1 || page.Items[0].Name != "Uploaded" {
		t.Errorf("listing after upload = %+v, want the uploaded asset", page.Items)
	}
}
