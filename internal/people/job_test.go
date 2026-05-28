package people_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/people"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// TestFaceIndexJobRoundTrip drives the split face-index job: the WORKER handler
// recognizes faces from a staged file and produces job Data; that Data round-
// trips through JSON (as the job API would carry it); the SERVE-side
// ResultHandler decodes it and assigns faces against the DB. The end state must
// match the all-in-one IndexAsset path: one face assigned to a new cluster.
func TestFaceIndexJobRoundTrip(t *testing.T) {
	repo, cat, _ := newRepo(t)
	seedAsset(t, cat, "asset-a")
	ctx := context.Background()

	// Worker side: a Pipeline that only needs the Recognizer (no DB on the worker).
	rec := newFakeRecognizer()
	rec.queueDetection(catalog.Face{ExtRef: "F1", Confidence: 99})
	workerPipe := people.NewPipeline(rec, repo, nil, "coll")

	// Stage a source file for the worker handler to open.
	src := filepath.Join(t.TempDir(), "asset-a.jpg")
	if err := os.WriteFile(src, []byte("the-image-bytes"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	wh := people.WorkerHandler(workerPipe)
	res, err := wh(ctx, worker.JobView{ID: "j1", Kind: catalog.JobKindFaceIndex, AssetID: "asset-a", MediaPath: "pictures/2021/asset-a.jpg"}, src)
	if err != nil {
		t.Fatalf("worker handler: %v", err)
	}
	if len(res.Data) == 0 {
		t.Fatalf("worker handler produced no data")
	}

	// Simulate the job API carrying Data as JSON (the worker posts it, the server
	// decodes it into catalog.JobResult.Data).
	wire, err := json.Marshal(res.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var jobData map[string]any
	if err := json.Unmarshal(wire, &jobData); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}

	// Serve side: ResultHandler over the real repo.
	servePipe := people.NewPipeline(rec, repo, nil, "coll")
	rh := people.ResultHandler(servePipe)
	job := catalog.Job{ID: "j1", Kind: catalog.JobKindFaceIndex, AssetID: "asset-a"}
	if err := rh(ctx, job, catalog.JobResult{Data: jobData}); err != nil {
		t.Fatalf("result handler: %v", err)
	}

	faces, err := repo.FacesForAsset(ctx, "asset-a")
	if err != nil {
		t.Fatalf("FacesForAsset: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("expected 1 assigned face from the split job, got %d", len(faces))
	}
	if faces[0].PersonID == "" {
		t.Fatalf("face should be assigned to a cluster, got unassigned")
	}
	persons, _ := repo.ListPersons(ctx)
	if len(persons) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(persons))
	}
}

// TestServeResultHandlerNeedsNoRecognizer locks the serve-side contract: the
// face-index ResultHandler runs only the assignment half (DB writes), so a
// serve Pipeline built with a NIL recognizer works. This is why serve can
// register the face-index handler while staying AWS-free (only the worker, which
// runs Recognize, needs a real recognizer). Mirrors the #16 lesson: an
// unregistered result handler silently drops worker output.
func TestServeResultHandlerNeedsNoRecognizer(t *testing.T) {
	repo, cat, _ := newRepo(t)
	seedAsset(t, cat, "asset-a")
	ctx := context.Background()

	// Serve pipeline: nil recognizer, real repo + search sink.
	sink := newFakeSearchSink()
	servePipe := people.NewPipeline(nil, repo, sink, "coll")
	rh := people.ResultHandler(servePipe)

	// Hand it Data as if a worker had recognized one face that matched nobody.
	detected := []people.DetectedFace{{Face: catalog.Face{ExtRef: "F1", Confidence: 99}}}
	data := people.EncodeDetectedForTest("hash-1", detected)

	job := catalog.Job{ID: "j1", Kind: catalog.JobKindFaceIndex, AssetID: "asset-a"}
	if err := rh(ctx, job, catalog.JobResult{Data: data}); err != nil {
		t.Fatalf("serve result handler (nil recognizer): %v", err)
	}
	faces, err := repo.FacesForAsset(ctx, "asset-a")
	if err != nil {
		t.Fatalf("FacesForAsset: %v", err)
	}
	if len(faces) != 1 || faces[0].PersonID == "" {
		t.Fatalf("serve assignment should persist 1 assigned face, got %+v", faces)
	}
}
