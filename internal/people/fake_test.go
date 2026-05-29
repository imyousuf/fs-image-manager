package people_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/people"
)

// fakeRecognizer is a deterministic, network-free people.Recognizer for the
// default test suite. DetectAndEmbed returns a scripted set of faces per call
// (one script entry per IndexAsset, in call order); SearchSimilar returns the
// scripted candidates for a given face ext ref. This lets a test drive an exact
// detect -> match -> cluster -> assign -> unknown sequence with no AWS.
type fakeRecognizer struct {
	mu sync.Mutex
	// detections is consumed front-to-back: each IndexAsset/Recognize pops one
	// entry as that image's detected faces.
	detections [][]catalog.Face
	// similar maps a face ext ref to the candidates SearchSimilar returns for it.
	similar map[string][]people.Candidate
}

func newFakeRecognizer() *fakeRecognizer {
	return &fakeRecognizer{similar: map[string][]people.Candidate{}}
}

// queueDetection scripts the faces the next Recognize call will return.
func (f *fakeRecognizer) queueDetection(faces ...catalog.Face) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detections = append(f.detections, faces)
}

// setSimilar scripts the candidates SearchSimilar returns for a face ext ref.
func (f *fakeRecognizer) setSimilar(extRef string, cands ...people.Candidate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.similar[extRef] = cands
}

func (f *fakeRecognizer) DetectAndEmbed(_ context.Context, _ io.Reader) ([]catalog.Face, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.detections) == 0 {
		return nil, nil
	}
	faces := f.detections[0]
	f.detections = f.detections[1:]
	return faces, nil
}

func (f *fakeRecognizer) SearchSimilar(_ context.Context, extRef string) ([]people.Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.similar[extRef], nil
}

// fakeSearchSink records the persons string the pipeline pushes per asset, so a
// test can assert "photos of X" text without a real search index.
type fakeSearchSink struct {
	mu      sync.Mutex
	persons map[string]string
}

func newFakeSearchSink() *fakeSearchSink { return &fakeSearchSink{persons: map[string]string{}} }

func (s *fakeSearchSink) UpsertPersons(_ context.Context, assetID, persons string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persons[assetID] = persons
	return nil
}

func (s *fakeSearchSink) get(assetID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persons[assetID]
}

// newRepo opens a fresh temp DB (migrations applied) and returns a people.Repo
// plus the catalog repo (to plant the asset rows faces FK to) and the conn.
func newRepo(t *testing.T) (*people.Repo, *catalog.Repo, *sql.DB) {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "people_test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return people.NewRepo(conn), catalog.NewRepo(conn, media.ClassifyExt), conn
}

// seedAsset plants a minimal image asset so a face row can reference it.
func seedAsset(t *testing.T, repo *catalog.Repo, id string) {
	t.Helper()
	a := catalog.Asset{
		ID:          id,
		Alias:       "pictures",
		Dir:         "2021",
		BaseName:    id,
		Kind:        catalog.AssetKindImage,
		DisplayPath: catalog.MediaPath(fmt.Sprintf("pictures/2021/%s.jpg", id)),
		Files: []catalog.File{
			{MediaPath: catalog.MediaPath(fmt.Sprintf("pictures/2021/%s.jpg", id)), Kind: catalog.FileKindJPG},
		},
	}
	if err := repo.PutAsset(context.Background(), a); err != nil {
		t.Fatalf("seedAsset %s: %v", id, err)
	}
}
