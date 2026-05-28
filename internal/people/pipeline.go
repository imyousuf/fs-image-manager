package people

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// The pipeline is the heart of people recognition (docs/TECH_SPEC.md section
// 9.2). It runs per asset as a face-index job:
//
//  1. Hash the source bytes. If this asset's exact bytes were already indexed
//     (the enrichment-run ledger), skip -- only new/changed media is indexed, so
//     re-running over a large DSLR library is bounded.
//  2. Detect + embed every face (Recognizer.DetectAndEmbed). Each face is now a
//     persisted vector in the collection identified by an external ref (FaceId).
//  3. For each new face, search the collection for already-indexed faces of the
//     same person (Recognizer.SearchSimilar). The best match that we already
//     know the person of (via the repo: ext_ref -> person_id) decides the
//     assignment.
//  4. Assign: matched -> the known Person; unmatched -> a fresh "unknown" cluster
//     (so a never-before-seen person still gets grouped, pending a name). A
//     subsequent photo of that same person then matches the unknown cluster and
//     auto-joins it. The user later names the cluster (RenamePerson) and every
//     face -- past and future -- carries that name into search.
//  5. Refresh the asset's search "persons" column so "photos of X" matches by
//     name once a cluster is named.
//
// Clustering quality, not gimmickry: assignment is gated by a similarity
// threshold (the Recognizer's), unmatched faces are NOT force-merged, and the
// first face of an unknown person seeds its own cluster rather than being
// dropped.

// Recognizer is the face backend the pipeline drives. It is a superset of the
// catalog.FaceRecognizer contract: DetectAndEmbed matches the shared port, while
// SearchSimilar exposes the collection-search primitive the pipeline needs to
// resolve a face to a person via the repo (rather than relying on the backend to
// know our person ids). RekognitionRecognizer and the test fake implement it.
type Recognizer interface {
	// DetectAndEmbed detects faces in the image and embeds/indexes each, returning
	// one Face per detection with ExtRef set to the backend's face reference.
	DetectAndEmbed(ctx context.Context, r io.Reader) ([]catalog.Face, error)
	// SearchSimilar returns external face refs already in the collection that match
	// the indexed face faceExtRef, best (most similar) first. The query face's own
	// ref is excluded. An empty slice means no match above the backend threshold.
	SearchSimilar(ctx context.Context, faceExtRef string) ([]Candidate, error)
}

// Candidate is one collection-search hit: another face's external ref and its
// similarity (0..100) to the query face.
type Candidate struct {
	ExtRef     string
	Similarity float64
}

// PersonStore is the slice of the repo the pipeline persists through. *Repo
// satisfies it.
type PersonStore interface {
	FacesForAsset(ctx context.Context, assetID string) ([]catalog.Face, error)
	DeleteFacesForAsset(ctx context.Context, assetID string) error
	PutFace(ctx context.Context, f catalog.Face, contentHash string) error
	AssignFace(ctx context.Context, faceID, personID string) error
	PutPerson(ctx context.Context, p catalog.Person, extCollection string) error
	GetPerson(ctx context.Context, id string) (catalog.Person, error)
	SetCoverFace(ctx context.Context, personID, faceID string) error
	PersonForExtRef(ctx context.Context, extRef string) (string, bool, error)
	ListPersonNames(ctx context.Context, personIDs []string) (map[string]string, error)
	HasRun(ctx context.Context, assetID, kind, contentHash string) (bool, error)
	RecordRun(ctx context.Context, assetID, kind, contentHash string) error
}

// SearchPersonSink is the slice of the search index the pipeline writes person
// names into (the FTS "persons" column), so "photos of <name>" full-text matches.
// *search.Index satisfies it via UpsertEnrichmentPersons (see indexer.go shim).
type SearchPersonSink interface {
	UpsertPersons(ctx context.Context, assetID, persons string) error
}

// Pipeline wires a Recognizer to the people repo (and optionally the search
// index) and runs the per-asset face-index flow.
type Pipeline struct {
	rec        Recognizer
	repo       PersonStore
	search     SearchPersonSink // optional; nil disables the search-name push
	collection string           // ext_collection tag stamped on created persons
}

// NewPipeline builds a Pipeline. search may be nil (no FTS person push, e.g. in
// the worker process which has no search index); collection tags persons with
// the backend collection they belong to.
func NewPipeline(rec Recognizer, repo PersonStore, search SearchPersonSink, collection string) *Pipeline {
	return &Pipeline{rec: rec, repo: repo, search: search, collection: collection}
}

// DetectedFace pairs a detected+embedded face with the collection-search
// candidates for it. It is the unit produced by the recognition half (Recognize,
// which makes the AWS/GPU calls) and consumed by the assignment half (Assign,
// which makes the DB writes). Splitting at this boundary lets the face-index job
// run recognition on the worker (where AWS creds / a local GPU model live) and
// assignment on the serve host (where the people DB lives), shuttling
// []DetectedFace between them as job Data -- while the all-in-one IndexAsset path
// (tests, or a Rekognition-on-serve deployment) reuses the exact same two halves.
type DetectedFace struct {
	Face       catalog.Face
	Candidates []Candidate
}

// IndexAsset runs the whole face-index pipeline for one asset over its source
// bytes (recognition + assignment in-process). assetID identifies the asset; r
// streams its display image. It is idempotent and content-hash cached: an asset
// whose exact bytes were already indexed is a no-op; re-indexing changed bytes
// replaces the asset's prior faces.
func (p *Pipeline) IndexAsset(ctx context.Context, assetID string, r io.Reader) error {
	img, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("people: read asset %s: %w", assetID, err)
	}
	hash := contentHash(img)

	done, err := p.repo.HasRun(ctx, assetID, catalog.JobKindFaceIndex, hash)
	if err != nil {
		return err
	}
	if done {
		return nil // unchanged bytes already indexed; bounded re-index cost
	}

	detected, err := p.Recognize(ctx, img)
	if err != nil {
		return err
	}
	return p.Assign(ctx, assetID, hash, detected)
}

// Recognize is the recognition half: detect + embed every face in the image and,
// for each, fetch its collection-search candidates. It performs no DB writes, so
// it can run on the worker. Returns one DetectedFace per detected face.
func (p *Pipeline) Recognize(ctx context.Context, img []byte) ([]DetectedFace, error) {
	faces, err := p.rec.DetectAndEmbed(ctx, byteReader(img))
	if err != nil {
		return nil, fmt.Errorf("people: detect faces: %w", err)
	}
	out := make([]DetectedFace, 0, len(faces))
	for _, f := range faces {
		cands, serr := p.rec.SearchSimilar(ctx, f.ExtRef)
		if serr != nil {
			return nil, fmt.Errorf("people: search similar for %s: %w", f.ExtRef, serr)
		}
		out = append(out, DetectedFace{Face: f, Candidates: cands})
	}
	return out, nil
}

// Assign is the assignment half: it persists the detected faces, clusters each
// into a known Person (via a candidate whose person we already know) or a fresh
// "unknown" cluster, seeds cover faces, pushes person names into search, and
// records the content-hash run. All writes go through the repo, so this is the
// serve-side half of the split job. It replaces any prior faces for the asset
// (re-index safety) and is the single place the clustering policy lives.
func (p *Pipeline) Assign(ctx context.Context, assetID, hash string, detected []DetectedFace) error {
	if err := p.repo.DeleteFacesForAsset(ctx, assetID); err != nil {
		return err
	}

	touched := make(map[string]struct{}) // person ids appearing in this asset
	for i, d := range detected {
		f := d.Face
		f.AssetID = assetID
		f.ID = faceID(assetID, f.ExtRef, i)

		personID, err := p.resolvePerson(ctx, f, d.Candidates)
		if err != nil {
			return err
		}
		f.PersonID = personID

		if err := p.repo.PutFace(ctx, f, hash); err != nil {
			return err
		}
		touched[personID] = struct{}{}

		// Seed a new cluster's cover face with its first member.
		if err := p.maybeSetCover(ctx, personID, f.ID); err != nil {
			return err
		}
	}

	if err := p.pushPersonNames(ctx, assetID, touched); err != nil {
		return err
	}
	return p.repo.RecordRun(ctx, assetID, catalog.JobKindFaceIndex, hash)
}

// resolvePerson decides which person a detected face belongs to from its
// collection-search candidates: for the best (most similar) candidate whose
// person we already know, it returns that person id; if none resolves to a known
// person, it opens a new "unknown" cluster and returns its id. The face's own
// ExtRef is skipped so a face never matches itself.
func (p *Pipeline) resolvePerson(ctx context.Context, f catalog.Face, cands []Candidate) (string, error) {
	// Candidates are best-first; take the first whose person we already know.
	ordered := append([]Candidate(nil), cands...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Similarity > ordered[j].Similarity })
	for _, c := range ordered {
		if c.ExtRef == f.ExtRef {
			continue
		}
		pid, ok, err := p.repo.PersonForExtRef(ctx, c.ExtRef)
		if err != nil {
			return "", err
		}
		if ok && pid != "" {
			return pid, nil
		}
	}
	return p.newUnknownCluster(ctx, f)
}

// newUnknownCluster creates a fresh, unnamed person cluster for a face that
// matched no known person. Its id is derived from the seed face's external ref so
// it is stable and collision-free.
func (p *Pipeline) newUnknownCluster(ctx context.Context, f catalog.Face) (string, error) {
	pid := personIDFor(f.ExtRef)
	if err := p.repo.PutPerson(ctx, catalog.Person{ID: pid, Name: ""}, p.collection); err != nil {
		return "", err
	}
	return pid, nil
}

// maybeSetCover sets a person's cover face to faceID when the person has none yet
// (a just-created unknown cluster), so the People view has a thumbnail to show.
func (p *Pipeline) maybeSetCover(ctx context.Context, personID, faceID string) error {
	per, err := p.repo.GetPerson(ctx, personID)
	if err != nil {
		return err
	}
	if per.CoverFaceID == "" {
		return p.repo.SetCoverFace(ctx, personID, faceID)
	}
	return nil
}

// pushPersonNames refreshes the asset's search "persons" FTS column from the
// names of the persons appearing in it, so a named person's photos are
// full-text findable by name. Unnamed (unknown) clusters contribute nothing
// (there is no name to search yet). A nil search sink disables this.
func (p *Pipeline) pushPersonNames(ctx context.Context, assetID string, personIDs map[string]struct{}) error {
	if p.search == nil || len(personIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(personIDs))
	for id := range personIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	names, err := p.repo.ListPersonNames(ctx, ids)
	if err != nil {
		return err
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			parts = append(parts, n)
		}
	}
	sort.Strings(parts)
	return p.search.UpsertPersons(ctx, assetID, strings.Join(parts, " "))
}

// --- id derivation -----------------------------------------------------------

// contentHash is the hex SHA-256 of the source bytes -- the face-index cache key.
func contentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// faceID is a stable id for a detected face: derived from the asset, the
// backend's face ref and the detection index so re-detecting yields the same id
// only when the backend assigns the same ref (it does not, since each IndexFaces
// mints fresh FaceIds -- hence DeleteFacesForAsset before re-index).
func faceID(assetID, extRef string, idx int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", assetID, extRef, idx)))
	return hex.EncodeToString(sum[:])[:24]
}

// personIDFor derives a stable cluster id from the seed face's external ref, so
// the same seed never creates two clusters.
func personIDFor(extRef string) string {
	sum := sha256.Sum256([]byte("person|" + extRef))
	return hex.EncodeToString(sum[:])[:24]
}

// byteReader hands the in-memory image bytes to the recognizer as a reader.
func byteReader(b []byte) io.Reader { return bytes.NewReader(b) }
