package people

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// face-index is a two-sided job (docs/TECH_SPEC.md section 9.2), split at the
// DetectedFace boundary:
//
//   - On the WORKER (AWS creds / future local GPU model): WorkerHandler stages
//     the source, runs the recognition half (Pipeline.Recognize -> detect + embed
//     + collection search) and ships the []DetectedFace plus the content hash back
//     as structured job Data. No DB, no derivative file.
//   - On the SERVE host (the people DB): ResultHandler decodes that Data and runs
//     the assignment half (Pipeline.Assign -> cluster, assign, cover, search-name
//     push, record run). The jobs Registry invokes it when the worker posts the
//     result.
//
// The two halves are decoupled by the job queue; the shared wire shape is
// encode/decodeDetected below.

// Result Data keys for the recognition output carried over the job API.
const (
	dataKeyContentHash = "contentHash"
	dataKeyFaces       = "faces"
)

// WorkerHandler returns a worker.Handler for the face-index kind. It needs a
// Pipeline whose repo is unused on this side (a no-op/zero store is fine) since
// Recognize performs no DB access; in practice the worker builds a Pipeline with
// just the Recognizer. Register it on the worker:
//
//	w.RegisterHandler(catalog.JobKindFaceIndex, people.WorkerHandler(pipeline))
func WorkerHandler(p *Pipeline) worker.Handler {
	return func(ctx context.Context, job worker.JobView, sourcePath string) (worker.HandlerResult, error) {
		f, err := os.Open(sourcePath) //nolint:gosec // sourcePath is the worker's own staged temp file
		if err != nil {
			return worker.HandlerResult{}, fmt.Errorf("people: open source for %s: %w", job.AssetID, err)
		}
		defer func() { _ = f.Close() }()

		img, err := io.ReadAll(f)
		if err != nil {
			return worker.HandlerResult{}, fmt.Errorf("people: read source for %s: %w", job.AssetID, err)
		}
		detected, err := p.Recognize(ctx, img)
		if err != nil {
			return worker.HandlerResult{}, err
		}
		return worker.HandlerResult{Data: encodeDetected(contentHash(img), detected)}, nil
	}
}

// ResultHandler returns a jobs.ResultHandler (jobs.Registry callback) for the
// face-index kind: it decodes the recognition output from the job Data and runs
// the assignment half against the DB. Register it on the serve host:
//
//	reg.Register(catalog.JobKindFaceIndex, people.ResultHandler(pipeline))
func ResultHandler(p *Pipeline) func(ctx context.Context, job catalog.Job, result catalog.JobResult) error {
	return func(ctx context.Context, job catalog.Job, result catalog.JobResult) error {
		hash, detected := decodeDetected(result.Data)
		if hash == "" {
			// Fall back to the job's recorded content hash if the worker omitted it.
			hash = job.Params["contentHash"]
		}
		return p.Assign(ctx, job.AssetID, hash, detected)
	}
}

// --- wire shape --------------------------------------------------------------

// wireFace is the JSON shape of one DetectedFace carried over the job API.
type wireFace struct {
	ExtRef     string      `json:"extRef"`
	BBox       [4]float64  `json:"bbox"`
	Confidence float64     `json:"confidence"`
	Candidates []Candidate `json:"candidates"`
}

// encodeDetected serializes the recognition output (content hash + detected
// faces) into the JSON-able Data map carried by the job result. The faces are
// marshalled to JSON and stored as a string so the generic map[string]any
// survives the job API round-trip without bespoke per-field decoding.
func encodeDetected(hash string, detected []DetectedFace) map[string]any {
	faces := make([]wireFace, len(detected))
	for i, d := range detected {
		faces[i] = wireFace{
			ExtRef:     d.Face.ExtRef,
			BBox:       d.Face.BBox,
			Confidence: d.Face.Confidence,
			Candidates: d.Candidates,
		}
	}
	b, _ := json.Marshal(faces) // wireFace has no unmarshalable fields
	return map[string]any{
		dataKeyContentHash: hash,
		dataKeyFaces:       string(b),
	}
}

// decodeDetected reverses encodeDetected, tolerating a nil/partial map.
func decodeDetected(data map[string]any) (string, []DetectedFace) {
	hash, _ := data[dataKeyContentHash].(string)
	raw, _ := data[dataKeyFaces].(string)
	if raw == "" {
		return hash, nil
	}
	var faces []wireFace
	if err := json.Unmarshal([]byte(raw), &faces); err != nil {
		return hash, nil
	}
	out := make([]DetectedFace, len(faces))
	for i, wf := range faces {
		out[i] = DetectedFace{
			Face: catalog.Face{
				ExtRef:     wf.ExtRef,
				BBox:       wf.BBox,
				Confidence: wf.Confidence,
			},
			Candidates: wf.Candidates,
		}
	}
	return hash, out
}
