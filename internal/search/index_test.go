package search_test

import (
	"context"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/metadata"
	"github.com/imyousuf/fs-image-manager/internal/search"
)

// fakeExtractor returns a canned Meta per asset id, so IndexAsset can be tested
// without touching the filesystem or EXIF.
type fakeExtractor struct {
	byID map[string]metadata.Meta
}

func (f fakeExtractor) Extract(_ context.Context, a catalog.Asset) (metadata.Meta, error) {
	return f.byID[a.ID], nil
}

func imageAsset(id, alias, base string) catalog.Asset {
	return catalog.Asset{
		ID: id, Alias: alias, Dir: "2021", BaseName: base, Kind: catalog.AssetKindImage,
		DisplayPath: catalog.MediaPath(alias + "/2021/" + base + ".jpg"),
		Files:       []catalog.File{{MediaPath: catalog.MediaPath(alias + "/2021/" + base + ".jpg"), Kind: catalog.FileKindJPG}},
	}
}

// TestIndexAssetEndToEnd: the Indexer extracts metadata, persists it, refreshes
// the FTS row (searchable by name + camera) and propagates CapturedAt to the
// catalog asset.
func TestIndexAssetEndToEnd(t *testing.T) {
	ix, repo, _ := newIndex(t)
	ctx := context.Background()

	a := imageAsset("a1", "pictures", "harbor_dawn")
	if err := repo.PutAsset(ctx, a); err != nil {
		t.Fatalf("put asset: %v", err)
	}

	captured := time.Date(2021, 4, 1, 8, 0, 0, 0, time.UTC)
	ext := fakeExtractor{byID: map[string]metadata.Meta{
		"a1": {CameraModel: "Canon EOS R5", Lens: "RF 15-35mm", CapturedAt: &captured, Source: metadata.SourceEXIF},
	}}
	idx := search.NewIndexer(ix, ext, repo)

	if err := idx.IndexAsset(ctx, a); err != nil {
		t.Fatalf("index asset: %v", err)
	}

	// captured_at propagated to the catalog.
	got, err := repo.GetAsset(ctx, "a1")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if got.CapturedAt == nil || !got.CapturedAt.Equal(captured) {
		t.Fatalf("catalog capturedAt = %v, want %v", got.CapturedAt, captured)
	}

	// Searchable by name token.
	res, err := ix.Search(ctx, search.Query{Text: "harbor"})
	if err != nil {
		t.Fatalf("search name: %v", err)
	}
	if len(res.AssetIDs) != 1 || res.AssetIDs[0] != "a1" {
		t.Fatalf("name search = %v, want [a1]", res.AssetIDs)
	}

	// Searchable by camera facet.
	res, err = ix.Search(ctx, search.Query{Camera: "Canon EOS R5"})
	if err != nil {
		t.Fatalf("search camera: %v", err)
	}
	if len(res.AssetIDs) != 1 || res.AssetIDs[0] != "a1" {
		t.Fatalf("camera search = %v, want [a1]", res.AssetIDs)
	}

	// Metadata round-trips.
	m, ok, err := ix.GetMetadata(ctx, "a1")
	if err != nil || !ok {
		t.Fatalf("get metadata: ok=%v err=%v", ok, err)
	}
	if m.Lens != "RF 15-35mm" {
		t.Errorf("lens = %q, want RF 15-35mm", m.Lens)
	}
}

// TestEnrichmentPreservedAcrossReindex: ai-people fills labels/persons via
// UpsertEnrichment; a later metadata re-index (name/camera/lens) must NOT wipe
// those columns. Search by a label and by a person name still hits the asset.
func TestEnrichmentPreservedAcrossReindex(t *testing.T) {
	ix, repo, _ := newIndex(t)
	ctx := context.Background()

	a := imageAsset("a2", "pictures", "group_photo")
	if err := repo.PutAsset(ctx, a); err != nil {
		t.Fatalf("put asset: %v", err)
	}
	ext := fakeExtractor{byID: map[string]metadata.Meta{
		"a2": {CameraModel: "Canon EOS R5", Source: metadata.SourceEXIF},
	}}
	idx := search.NewIndexer(ix, ext, repo)
	if err := idx.IndexAsset(ctx, a); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// ai-people enriches: a "dog" label and a person "Alice".
	if err := ix.UpsertEnrichment(ctx, "a2", "dog outdoor", "a happy dog", "", "Alice Smith"); err != nil {
		t.Fatalf("enrich: %v", err)
	}

	// Re-index metadata (simulating a re-scan): enrichment must survive.
	if err := idx.IndexAsset(ctx, a); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	if res, _ := ix.Search(ctx, search.Query{Text: "dog"}); len(res.AssetIDs) != 1 || res.AssetIDs[0] != "a2" {
		t.Fatalf("label search after reindex = %v, want [a2]", res.AssetIDs)
	}
	if res, _ := ix.Search(ctx, search.Query{Person: "Alice"}); len(res.AssetIDs) != 1 || res.AssetIDs[0] != "a2" {
		t.Fatalf("person search after reindex = %v, want [a2]", res.AssetIDs)
	}
	// And name search still works (core columns intact).
	if res, _ := ix.Search(ctx, search.Query{Text: "group_photo"}); len(res.AssetIDs) != 1 {
		t.Fatalf("name search after reindex = %v, want [a2]", res.AssetIDs)
	}
}

// TestColumnScopedEnrichmentNonDestructive verifies the two ai-people jobs
// (enrich-ai -> UpsertEnrichmentText; face-index -> UpsertPersons) write disjoint
// FTS columns without clobbering each other, in either order, and that a metadata
// re-index preserves both. This is the seam ai-people (task #6) depends on.
func TestColumnScopedEnrichmentNonDestructive(t *testing.T) {
	ix, repo, _ := newIndex(t)
	ctx := context.Background()

	a := imageAsset("c1", "pictures", "reunion")
	if err := repo.PutAsset(ctx, a); err != nil {
		t.Fatalf("put asset: %v", err)
	}
	ext := fakeExtractor{byID: map[string]metadata.Meta{
		"c1": {CameraModel: "Canon EOS R5", Source: metadata.SourceEXIF},
	}}
	idx := search.NewIndexer(ix, ext, repo)
	if err := idx.IndexAsset(ctx, a); err != nil {
		t.Fatalf("index: %v", err)
	}

	// face-index runs first: set persons only.
	if err := ix.UpsertPersons(ctx, "c1", "Bob Jones"); err != nil {
		t.Fatalf("upsert persons: %v", err)
	}
	// enrich-ai runs second: set labels/caption/ocr only. Must NOT wipe persons.
	if err := ix.UpsertEnrichmentText(ctx, "c1", "cake party", "birthday cake", "Happy Birthday"); err != nil {
		t.Fatalf("upsert enrichment text: %v", err)
	}

	assertHit := func(q search.Query, label string) {
		res, err := ix.Search(ctx, q)
		if err != nil {
			t.Fatalf("%s search: %v", label, err)
		}
		if len(res.AssetIDs) != 1 || res.AssetIDs[0] != "c1" {
			t.Fatalf("%s search = %v, want [c1]", label, res.AssetIDs)
		}
	}
	// All columns coexist: person, label, caption, OCR text, and the name core.
	assertHit(search.Query{Person: "Bob"}, "person-after-text")
	assertHit(search.Query{Text: "cake"}, "label")
	assertHit(search.Query{Text: "birthday"}, "caption")
	assertHit(search.Query{Text: "Happy"}, "ocr")
	assertHit(search.Query{Text: "reunion"}, "name")

	// Now exercise the reverse order on the same row: re-running face-index with a
	// corrected name must preserve the enrich-ai text.
	if err := ix.UpsertPersons(ctx, "c1", "Bob Jones Sr"); err != nil {
		t.Fatalf("re-upsert persons: %v", err)
	}
	assertHit(search.Query{Person: "Sr"}, "updated-person")
	assertHit(search.Query{Text: "birthday"}, "caption-after-person-update")

	// A metadata re-index preserves both enrichment families and the core.
	if err := idx.IndexAsset(ctx, a); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	assertHit(search.Query{Person: "Bob"}, "person-after-reindex")
	assertHit(search.Query{Text: "cake"}, "label-after-reindex")
	assertHit(search.Query{Text: "reunion"}, "name-after-reindex")
}

// fakeLibs / fakeLister back the backfill test without a resolver/config.
type fakeLibs struct{ libs []catalog.Library }

func (f fakeLibs) Libraries() []catalog.Library { return f.libs }

func TestBackfill(t *testing.T) {
	ix, repo, _ := newIndex(t)
	ctx := context.Background()

	// Seed two catalog assets WITHOUT indexing them.
	for _, base := range []string{"alpha", "bravo"} {
		a := imageAsset(base, "pictures", base)
		if err := repo.PutAsset(ctx, a); err != nil {
			t.Fatalf("put %s: %v", base, err)
		}
	}

	cap1 := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	ext := fakeExtractor{byID: map[string]metadata.Meta{
		"alpha": {CameraModel: "Canon EOS R5", CapturedAt: &cap1, Source: metadata.SourceEXIF},
		"bravo": {CameraModel: "NIKON D850", Source: metadata.SourceEXIF},
	}}
	idx := search.NewIndexer(ix, ext, repo)

	res, err := idx.Backfill(ctx, fakeLibs{libs: []catalog.Library{{Alias: "pictures"}}}, repo, nil)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.Indexed != 2 || res.Failed != 0 {
		t.Fatalf("backfill result = %+v, want 2 indexed 0 failed", res)
	}
	// Both now searchable by name.
	if r, _ := ix.Search(ctx, search.Query{Text: "alpha"}); len(r.AssetIDs) != 1 {
		t.Fatalf("alpha not indexed by backfill: %v", r.AssetIDs)
	}
	// Timeline picked up alpha's date.
	buckets, _ := ix.Timeline(ctx, search.TimelineQuery{})
	if len(buckets) != 1 || buckets[0].Date != "2020-01-02" {
		t.Fatalf("timeline after backfill = %+v", buckets)
	}
}
