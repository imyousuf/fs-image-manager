package people

import (
	"context"
	"errors"
	"io"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// LocalRecognizer is the alternative, fully-private face backend (docs/TECH_SPEC.md
// section 9.2): InsightFace-style detection (RetinaFace/SCRFD) + ArcFace
// embedding running on the GPU worker, with clustering done locally instead of in
// a managed cloud collection. It keeps faces of family/friends entirely on the
// user's hardware -- the trade-off being more to build (alignment, embedding
// model hosting, and an unlabeled-face clustering step such as HDBSCAN or
// Chinese-Whispers over the ArcFace vectors).
//
// This is a documented STUB. The pipeline is backend-agnostic (it drives the
// Recognizer interface), so a full LocalRecognizer can drop in without touching
// the clustering/assignment policy: it only needs to (1) detect + align faces and
// produce ArcFace embeddings in DetectAndEmbed, persisting each vector under a
// stable ExtRef, and (2) return cosine-nearest neighbours above a threshold from
// SearchSimilar. The serve-side Assign half (PersonForExtRef -> known person, else
// new unknown cluster) is then identical to the Rekognition path.
//
// Until the model host is wired in, both methods return ErrLocalRecognizerUnimplemented
// so a misconfiguration fails loudly rather than silently producing no faces.
type LocalRecognizer struct {
	// Endpoint is the embedding/detection model server (e.g. an InsightFace HTTP
	// service on the GPU box). Held for the eventual implementation.
	Endpoint string
	// MatchThreshold is the cosine-similarity floor for SearchSimilar (0..1).
	MatchThreshold float64
}

// ErrLocalRecognizerUnimplemented is returned by the LocalRecognizer stub until
// the InsightFace/ArcFace model host is implemented.
var ErrLocalRecognizerUnimplemented = errors.New("people: local (InsightFace/ArcFace) recognizer not yet implemented; use the Rekognition backend")

// Compile-time checks that the stub still satisfies the contracts it will fill.
var (
	_ catalog.FaceRecognizer = (*LocalRecognizer)(nil)
	_ Recognizer             = (*LocalRecognizer)(nil)
)

// DetectAndEmbed is unimplemented in the stub.
func (l *LocalRecognizer) DetectAndEmbed(_ context.Context, _ io.Reader) ([]catalog.Face, error) {
	return nil, ErrLocalRecognizerUnimplemented
}

// Match is unimplemented in the stub.
func (l *LocalRecognizer) Match(_ context.Context, _ catalog.Face) (string, float64, error) {
	return "", 0, ErrLocalRecognizerUnimplemented
}

// SearchSimilar is unimplemented in the stub.
func (l *LocalRecognizer) SearchSimilar(_ context.Context, _ string) ([]Candidate, error) {
	return nil, ErrLocalRecognizerUnimplemented
}
