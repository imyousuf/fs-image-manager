package search_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/metadata"
	"github.com/imyousuf/fs-image-manager/internal/search"
)

// seedAsset describes a row to plant in the catalog + metadata + FTS index.
type seedAsset struct {
	id, alias, dir, base, kind string
	camera, lens               string
	captured                   string // RFC3339 or "" for none
}

// newIndex opens a fresh temp DB (migrations applied) and returns an Index plus
// the catalog repo for seeding.
func newIndex(t *testing.T) (*search.Index, *catalog.Repo, *sql.DB) {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "search_test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return search.New(conn), catalog.NewRepo(conn, media.ClassifyExt), conn
}

// seed plants an asset (catalog) and its metadata + FTS row via the Index.
func seed(t *testing.T, ix *search.Index, repo *catalog.Repo, s seedAsset) {
	t.Helper()
	ctx := context.Background()
	a := catalog.Asset{
		ID:          s.id,
		Alias:       s.alias,
		Dir:         s.dir,
		BaseName:    s.base,
		Kind:        s.kind,
		DisplayPath: catalog.MediaPath(s.alias + "/" + s.base + ".jpg"),
		Files: []catalog.File{
			{MediaPath: catalog.MediaPath(s.alias + "/" + s.base + ".jpg"), Kind: catalog.FileKindJPG},
		},
	}
	if err := repo.PutAsset(ctx, a); err != nil {
		t.Fatalf("seed PutAsset %s: %v", s.id, err)
	}

	m := metadata.Meta{
		CameraModel: s.camera,
		Lens:        s.lens,
		Source:      metadata.SourceEXIF,
	}
	if s.captured != "" {
		tm, err := time.Parse(time.RFC3339, s.captured)
		if err != nil {
			t.Fatalf("seed parse time %q: %v", s.captured, err)
		}
		m.CapturedAt = &tm
	}
	m.CameraMake = s.camera // model already carries make in these fixtures
	if err := ix.PutMetadata(ctx, a, m); err != nil {
		t.Fatalf("seed PutMetadata %s: %v", s.id, err)
	}
}

// corpus is the shared seed used by the search/timeline tests.
func corpus() []seedAsset {
	return []seedAsset{
		{id: "p1", alias: "pictures", dir: "2021", base: "sunset_beach", kind: catalog.AssetKindImage,
			camera: "Canon EOS R5", lens: "RF 24-70mm", captured: "2021-02-17T10:56:05Z"},
		{id: "p2", alias: "pictures", dir: "2021", base: "mountain_sunrise", kind: catalog.AssetKindImage,
			camera: "Canon EOS R5", lens: "RF 70-200mm", captured: "2021-02-18T07:30:00Z"},
		{id: "p3", alias: "pictures", dir: "2022", base: "city_lights", kind: catalog.AssetKindImage,
			camera: "NIKON D850", lens: "AF-S 50mm", captured: "2022-06-01T21:00:00Z"},
		{id: "v1", alias: "videos", dir: "clips", base: "beach_waves", kind: catalog.AssetKindVideo,
			camera: "", lens: "", captured: "2021-02-17T11:00:00Z"},
		{id: "p4", alias: "pictures", dir: "2022", base: "portrait", kind: catalog.AssetKindImage,
			camera: "Sony ILCE-7M3", lens: "FE 85mm", captured: ""}, // no capture time
	}
}

func seedCorpus(t *testing.T, ix *search.Index, repo *catalog.Repo) {
	t.Helper()
	for _, s := range corpus() {
		seed(t, ix, repo, s)
	}
}

func TestSearchFullText(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	ctx := context.Background()

	// "beach" matches the names sunset_beach (p1) and beach_waves (v1).
	res, err := ix.Search(ctx, search.Query{Text: "beach"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	got := idSet(res.AssetIDs)
	if !got["p1"] || !got["v1"] {
		t.Fatalf("beach search = %v, want p1 and v1", res.AssetIDs)
	}
	if got["p2"] || got["p3"] {
		t.Fatalf("beach search leaked unrelated assets: %v", res.AssetIDs)
	}
}

func TestSearchLensColumn(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	// "70-200mm" is in p2's lens column only.
	res, err := ix.Search(context.Background(), search.Query{Text: "70-200mm"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.AssetIDs) != 1 || res.AssetIDs[0] != "p2" {
		t.Fatalf("lens search = %v, want [p2]", res.AssetIDs)
	}
}

func TestSearchCameraFacet(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	res, err := ix.Search(context.Background(), search.Query{Camera: "Canon EOS R5"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	got := idSet(res.AssetIDs)
	if len(res.AssetIDs) != 2 || !got["p1"] || !got["p2"] {
		t.Fatalf("camera facet = %v, want p1,p2", res.AssetIDs)
	}
}

func TestSearchAliasFacet(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	res, err := ix.Search(context.Background(), search.Query{Alias: "videos"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.AssetIDs) != 1 || res.AssetIDs[0] != "v1" {
		t.Fatalf("alias facet = %v, want [v1]", res.AssetIDs)
	}
}

func TestSearchDateRange(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	from := time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2021, 2, 28, 23, 59, 59, 0, time.UTC)
	res, err := ix.Search(context.Background(), search.Query{From: from, To: to})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	got := idSet(res.AssetIDs)
	// p1,p2,v1 are in Feb 2021; p3 (2022) and p4 (no date) excluded.
	if len(res.AssetIDs) != 3 || !got["p1"] || !got["p2"] || !got["v1"] {
		t.Fatalf("date range = %v, want p1,p2,v1", res.AssetIDs)
	}
}

func TestSearchCombinedFacets(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	// text "sunrise" + camera Canon -> only p2.
	res, err := ix.Search(context.Background(), search.Query{Text: "sunrise", Camera: "Canon EOS R5"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.AssetIDs) != 1 || res.AssetIDs[0] != "p2" {
		t.Fatalf("combined facets = %v, want [p2]", res.AssetIDs)
	}
}

func TestSearchRecencyOrderNoText(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	// No text: dated assets newest-first, undated last.
	res, err := ix.Search(context.Background(), search.Query{Alias: "pictures"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// pictures with dates: p3 (2022-06), p2 (2021-02-18), p1 (2021-02-17); p4 undated last.
	want := []string{"p3", "p2", "p1", "p4"}
	if len(res.AssetIDs) != len(want) {
		t.Fatalf("got %v, want %v", res.AssetIDs, want)
	}
	for i, id := range want {
		if res.AssetIDs[i] != id {
			t.Fatalf("order[%d] = %s, want %s (full: %v)", i, res.AssetIDs[i], id, res.AssetIDs)
		}
	}
}

func TestSearchCursorPagination(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	ctx := context.Background()

	page1, err := ix.Search(ctx, search.Query{Alias: "pictures", Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.AssetIDs) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v, want 2 ids + cursor", page1)
	}
	page2, err := ix.Search(ctx, search.Query{Alias: "pictures", Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	// 4 pictures total -> page2 has the remaining 2 and no further cursor.
	if len(page2.AssetIDs) != 2 || page2.NextCursor != "" {
		t.Fatalf("page2 = %+v, want 2 ids + empty cursor", page2)
	}
	// No overlap between pages.
	seen := idSet(page1.AssetIDs)
	for _, id := range page2.AssetIDs {
		if seen[id] {
			t.Fatalf("page2 repeated %s from page1", id)
		}
	}
}

func TestSearchBadCursor(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	if _, err := ix.Search(context.Background(), search.Query{Cursor: "notanumber"}); err == nil {
		t.Fatal("expected error for malformed cursor")
	}
}

func idSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
