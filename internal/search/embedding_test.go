package search_test

import (
	"context"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// putBareAsset inserts a minimal asset so the embeddings FK (asset_id ->
// assets.id) is satisfied.
func putBareAsset(t *testing.T, repo *catalog.Repo, id string) {
	t.Helper()
	a := catalog.Asset{
		ID: id, Alias: "pictures", Dir: "e", BaseName: id, Kind: catalog.AssetKindImage,
		DisplayPath: catalog.MediaPath("pictures/" + id + ".jpg"),
		Files:       []catalog.File{{MediaPath: catalog.MediaPath("pictures/" + id + ".jpg"), Kind: catalog.FileKindJPG}},
	}
	if err := repo.PutAsset(context.Background(), a); err != nil {
		t.Fatalf("put asset %s: %v", id, err)
	}
}

func TestEmbeddingUpsertGetRoundTrip(t *testing.T) {
	ix, repo, _ := newIndex(t)
	ctx := context.Background()
	putBareAsset(t, repo, "e1")

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	if err := ix.UpsertEmbedding(ctx, "e1", vec); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, ok, err := ix.GetEmbedding(ctx, "e1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if len(got) != len(vec) {
		t.Fatalf("dim = %d, want %d", len(got), len(vec))
	}
	for i := range vec {
		if got[i] != vec[i] {
			t.Fatalf("vec[%d] = %v, want %v", i, got[i], vec[i])
		}
	}

	// Upsert overwrites.
	if err := ix.UpsertEmbedding(ctx, "e1", []float32{1, 0, 0, 0}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got2, _, _ := ix.GetEmbedding(ctx, "e1")
	if got2[0] != 1 || got2[1] != 0 {
		t.Fatalf("overwrite failed: %v", got2)
	}
}

func TestEmbeddingEmptyRejected(t *testing.T) {
	ix, repo, _ := newIndex(t)
	putBareAsset(t, repo, "e1")
	if err := ix.UpsertEmbedding(context.Background(), "e1", nil); err == nil {
		t.Fatal("expected error for empty embedding")
	}
}

func TestSearchSimilarTopK(t *testing.T) {
	ix, repo, _ := newIndex(t)
	ctx := context.Background()

	// Deterministic unit-ish vectors in 3-space. Query is aligned with q-axis.
	vectors := map[string][]float32{
		"near":     {1, 0, 0},     // cosine 1.0 with query
		"close":    {0.9, 0.1, 0}, // high cosine
		"orthog":   {0, 1, 0},     // cosine 0
		"opposite": {-1, 0, 0},    // cosine -1
	}
	for id, v := range vectors {
		putBareAsset(t, repo, id)
		if err := ix.UpsertEmbedding(ctx, id, v); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	matches, err := ix.SearchSimilar(ctx, []float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatalf("search similar: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("want top-3, got %d: %+v", len(matches), matches)
	}
	// Most similar first.
	if matches[0].AssetID != "near" {
		t.Errorf("top match = %s, want near", matches[0].AssetID)
	}
	if matches[1].AssetID != "close" {
		t.Errorf("second = %s, want close", matches[1].AssetID)
	}
	// Descending score order.
	for i := 1; i < len(matches); i++ {
		if matches[i-1].Score < matches[i].Score {
			t.Fatalf("scores not descending: %+v", matches)
		}
	}
	// "near" is identical direction -> cosine ~1.
	if matches[0].Score < 0.999 {
		t.Errorf("near score = %v, want ~1.0", matches[0].Score)
	}
}

func TestSearchSimilarDimMismatchSkipped(t *testing.T) {
	ix, repo, _ := newIndex(t)
	ctx := context.Background()
	putBareAsset(t, repo, "a3")
	putBareAsset(t, repo, "a4")
	if err := ix.UpsertEmbedding(ctx, "a3", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := ix.UpsertEmbedding(ctx, "a4", []float32{1, 0}); err != nil { // 2-dim
		t.Fatal(err)
	}
	// Query is 3-dim; the 2-dim row must be skipped, not error.
	matches, err := ix.SearchSimilar(ctx, []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("search similar: %v", err)
	}
	if len(matches) != 1 || matches[0].AssetID != "a3" {
		t.Fatalf("want only a3, got %+v", matches)
	}
}
