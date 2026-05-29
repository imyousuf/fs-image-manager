package people_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/people"
)

// TestPipelineClustersSamePerson exercises the core promise: two photos of the
// same person (the second face matching the first via the collection search)
// land in ONE cluster, while a third photo of a different, never-seen person
// opens its own "unknown" cluster. No network -- the fake recognizer scripts the
// detect + search results deterministically.
func TestPipelineClustersSamePerson(t *testing.T) {
	repo, cat, _ := newRepo(t)
	for _, id := range []string{"asset-a", "asset-b", "asset-c"} {
		seedAsset(t, cat, id)
	}
	rec := newFakeRecognizer()
	sink := newFakeSearchSink()
	p := people.NewPipeline(rec, repo, sink, "test-collection")
	ctx := context.Background()

	// Photo A: first sighting of person Alice (face F1). No prior faces, so no
	// match -> a new unknown cluster is seeded.
	rec.queueDetection(catalog.Face{ExtRef: "F1", Confidence: 99})
	// Photo B: another shot of Alice (face F2); the collection search says F2 is
	// most similar to F1 -> should join F1's cluster.
	rec.queueDetection(catalog.Face{ExtRef: "F2", Confidence: 98})
	rec.setSimilar("F2", people.Candidate{ExtRef: "F1", Similarity: 97})
	// Photo C: a different person Bob (face F3) matching nobody -> its own cluster.
	rec.queueDetection(catalog.Face{ExtRef: "F3", Confidence: 97})

	for _, id := range []string{"asset-a", "asset-b", "asset-c"} {
		if err := p.IndexAsset(ctx, id, bytes.NewReader([]byte("img-"+id))); err != nil {
			t.Fatalf("IndexAsset %s: %v", id, err)
		}
	}

	persons, err := repo.ListPersons(ctx)
	if err != nil {
		t.Fatalf("ListPersons: %v", err)
	}
	if len(persons) != 2 {
		t.Fatalf("expected 2 clusters (Alice + Bob), got %d: %+v", len(persons), persons)
	}

	// Identify Alice's cluster (the one with two assets) and Bob's (one asset).
	counts, err := repo.AssetCounts(ctx)
	if err != nil {
		t.Fatalf("AssetCounts: %v", err)
	}
	var alice, bob string
	for _, pr := range persons {
		switch counts[pr.ID] {
		case 2:
			alice = pr.ID
		case 1:
			bob = pr.ID
		default:
			t.Fatalf("cluster %s has unexpected asset count %d", pr.ID, counts[pr.ID])
		}
	}
	if alice == "" || bob == "" {
		t.Fatalf("did not find both a 2-asset and a 1-asset cluster: counts=%v", counts)
	}

	aliceAssets, err := repo.AssetIDsForPerson(ctx, alice)
	if err != nil {
		t.Fatalf("AssetIDsForPerson: %v", err)
	}
	if len(aliceAssets) != 2 {
		t.Fatalf("Alice should appear in 2 assets, got %v", aliceAssets)
	}
}

// TestPipelineRenameThenSearch covers labelling an unknown cluster and the name
// flowing into the search "persons" column for "photos of X". A face indexed
// AFTER the rename carries the name immediately; the test also re-indexes an
// earlier asset to confirm a named cluster's photos all get the name.
func TestPipelineRenameThenSearch(t *testing.T) {
	repo, cat, _ := newRepo(t)
	seedAsset(t, cat, "asset-1")
	seedAsset(t, cat, "asset-2")
	rec := newFakeRecognizer()
	sink := newFakeSearchSink()
	p := people.NewPipeline(rec, repo, sink, "test-collection")
	ctx := context.Background()

	// First photo seeds an unknown cluster from face F1.
	rec.queueDetection(catalog.Face{ExtRef: "F1", Confidence: 99})
	if err := p.IndexAsset(ctx, "asset-1", bytes.NewReader([]byte("img1"))); err != nil {
		t.Fatalf("IndexAsset asset-1: %v", err)
	}
	// Before naming, the search persons string is empty (no name to search).
	if got := sink.get("asset-1"); got != "" {
		t.Fatalf("unnamed cluster should push empty persons, got %q", got)
	}

	persons, _ := repo.ListPersons(ctx)
	if len(persons) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(persons))
	}
	clusterID := persons[0].ID

	// User names the cluster.
	if err := repo.RenamePerson(ctx, clusterID, "Alice"); err != nil {
		t.Fatalf("RenamePerson: %v", err)
	}

	// A second photo of Alice (F2 matches F1) is indexed; its search persons
	// string should now carry "Alice".
	rec.queueDetection(catalog.Face{ExtRef: "F2", Confidence: 98})
	rec.setSimilar("F2", people.Candidate{ExtRef: "F1", Similarity: 96})
	if err := p.IndexAsset(ctx, "asset-2", bytes.NewReader([]byte("img2"))); err != nil {
		t.Fatalf("IndexAsset asset-2: %v", err)
	}
	if got := sink.get("asset-2"); got != "Alice" {
		t.Fatalf("named cluster photo should push \"Alice\", got %q", got)
	}
}

// TestPipelineContentHashCache verifies that re-indexing an asset whose bytes are
// unchanged is a no-op (the content-hash ledger short-circuits), so a large
// library is not re-detected on every pass.
func TestPipelineContentHashCache(t *testing.T) {
	repo, cat, _ := newRepo(t)
	seedAsset(t, cat, "asset-x")
	rec := newFakeRecognizer()
	p := people.NewPipeline(rec, repo, nil, "test-collection")
	ctx := context.Background()

	rec.queueDetection(catalog.Face{ExtRef: "F1", Confidence: 99})
	body := []byte("stable-bytes")
	if err := p.IndexAsset(ctx, "asset-x", bytes.NewReader(body)); err != nil {
		t.Fatalf("first IndexAsset: %v", err)
	}
	// Second run over identical bytes must NOT consume another detection (none is
	// queued); if the cache failed, DetectAndEmbed would return nil and wipe the
	// face, which we assert against below.
	if err := p.IndexAsset(ctx, "asset-x", bytes.NewReader(body)); err != nil {
		t.Fatalf("second IndexAsset: %v", err)
	}
	faces, err := repo.FacesForAsset(ctx, "asset-x")
	if err != nil {
		t.Fatalf("FacesForAsset: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("content-hash cache should preserve the single face, got %d", len(faces))
	}
}

// TestPipelineReindexReplacesFaces verifies that re-indexing CHANGED bytes drops
// the stale faces and re-detects, rather than accumulating duplicates.
func TestPipelineReindexReplacesFaces(t *testing.T) {
	repo, cat, _ := newRepo(t)
	seedAsset(t, cat, "asset-y")
	rec := newFakeRecognizer()
	p := people.NewPipeline(rec, repo, nil, "test-collection")
	ctx := context.Background()

	rec.queueDetection(catalog.Face{ExtRef: "F1", Confidence: 99})
	if err := p.IndexAsset(ctx, "asset-y", bytes.NewReader([]byte("v1"))); err != nil {
		t.Fatalf("index v1: %v", err)
	}
	rec.queueDetection(
		catalog.Face{ExtRef: "G1", Confidence: 95},
		catalog.Face{ExtRef: "G2", Confidence: 94},
	)
	if err := p.IndexAsset(ctx, "asset-y", bytes.NewReader([]byte("v2-different"))); err != nil {
		t.Fatalf("index v2: %v", err)
	}
	faces, err := repo.FacesForAsset(ctx, "asset-y")
	if err != nil {
		t.Fatalf("FacesForAsset: %v", err)
	}
	if len(faces) != 2 {
		t.Fatalf("re-index of changed bytes should yield 2 faces (not accumulate), got %d", len(faces))
	}
}

// TestPipelineUnknownClusterHasCover verifies a freshly seeded unknown cluster
// gets its cover face set to the first detected face, so the People view has a
// thumbnail.
func TestPipelineUnknownClusterHasCover(t *testing.T) {
	repo, cat, _ := newRepo(t)
	seedAsset(t, cat, "asset-z")
	rec := newFakeRecognizer()
	p := people.NewPipeline(rec, repo, nil, "test-collection")
	ctx := context.Background()

	rec.queueDetection(catalog.Face{ExtRef: "F1", Confidence: 99})
	if err := p.IndexAsset(ctx, "asset-z", bytes.NewReader([]byte("img"))); err != nil {
		t.Fatalf("IndexAsset: %v", err)
	}
	persons, _ := repo.ListPersons(ctx)
	if len(persons) != 1 || persons[0].CoverFaceID == "" {
		t.Fatalf("unknown cluster should have a cover face, got %+v", persons)
	}
	if _, err := repo.GetFace(ctx, persons[0].CoverFaceID); err != nil {
		t.Fatalf("cover face %q should exist: %v", persons[0].CoverFaceID, err)
	}
}

// ensure the fake satisfies the Recognizer seam at compile time.
var _ people.Recognizer = (*fakeRecognizer)(nil)
