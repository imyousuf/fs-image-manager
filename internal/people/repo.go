// Package people is the high-quality people-recognition subsystem (see
// docs/TECH_SPEC.md section 9.2). It is deliberately its own pipeline, separate
// from generic enrichment (internal/enrich): generic VLMs describe an image but
// do not reliably re-identify a face across photos, so faces get a dedicated
// detect -> embed -> match/cluster -> assign-Person flow.
//
// The package has three layers:
//
//   - Repo: persistence for Person/Face (over the sqlc store), plus the
//     enrichment-run cache ledger shared with internal/enrich.
//   - FaceRecognizer implementations (catalog.FaceRecognizer): RekognitionRecognizer
//     (default, AWS Face Collections) and LocalRecognizer (InsightFace/ArcFace
//     stub). A FakeRecognizer for tests lives in fake_test helpers.
//   - The Pipeline that runs as a face-index job: detect+embed faces, match each
//     against the collection, assign to a known Person or open an "unknown"
//     cluster, and push person names into the search FTS so "photos of X" works.
//
// The HTTP People API (handlers.go) lists people, lists a person's photos and
// renames a cluster.
package people

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db/store"
)

// ErrNotFound is returned when a requested person/face does not exist.
var ErrNotFound = errors.New("people: not found")

// Repo is the people-recognition repository over the sqlc store. It persists and
// loads Person/Face rows and the enrichment-run cache ledger. It is safe for
// concurrent use to the extent the underlying *sql.DB is (the app caps SQLite to
// a single writer).
type Repo struct {
	q *store.Queries
}

// NewRepo builds a Repo over an open database connection (migrations already
// applied by internal/db).
func NewRepo(db store.DBTX) *Repo {
	return &Repo{q: store.New(db)}
}

// --- persons ----------------------------------------------------------------

// PutPerson upserts a person cluster (id, name, cover face, backend collection).
func (r *Repo) PutPerson(ctx context.Context, p catalog.Person, extCollection string) error {
	if err := r.q.UpsertPerson(ctx, store.UpsertPersonParams{
		ID:            p.ID,
		Name:          p.Name,
		CoverFaceID:   p.CoverFaceID,
		ExtCollection: extCollection,
	}); err != nil {
		return fmt.Errorf("people: upsert person %s: %w", p.ID, err)
	}
	return nil
}

// GetPerson loads a person by id, or ErrNotFound.
func (r *Repo) GetPerson(ctx context.Context, id string) (catalog.Person, error) {
	row, err := r.q.GetPerson(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.Person{}, fmt.Errorf("%w: person %s", ErrNotFound, id)
		}
		return catalog.Person{}, fmt.Errorf("people: get person %s: %w", id, err)
	}
	return catalog.Person{ID: row.ID, Name: row.Name, CoverFaceID: row.CoverFaceID}, nil
}

// ListPersons returns every person cluster, named ones first then unknown.
func (r *Repo) ListPersons(ctx context.Context) ([]catalog.Person, error) {
	rows, err := r.q.ListPersons(ctx)
	if err != nil {
		return nil, fmt.Errorf("people: list persons: %w", err)
	}
	out := make([]catalog.Person, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.Person{ID: row.ID, Name: row.Name, CoverFaceID: row.CoverFaceID})
	}
	return out, nil
}

// RenamePerson sets a person's display name. It returns ErrNotFound if no person
// has the given id.
func (r *Repo) RenamePerson(ctx context.Context, id, name string) error {
	n, err := r.q.RenamePerson(ctx, store.RenamePersonParams{Name: name, ID: id})
	if err != nil {
		return fmt.Errorf("people: rename person %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: person %s", ErrNotFound, id)
	}
	return nil
}

// SetCoverFace records the representative face for a person's cover thumbnail.
func (r *Repo) SetCoverFace(ctx context.Context, personID, faceID string) error {
	if err := r.q.SetPersonCoverFace(ctx, store.SetPersonCoverFaceParams{CoverFaceID: faceID, ID: personID}); err != nil {
		return fmt.Errorf("people: set cover face %s: %w", personID, err)
	}
	return nil
}

// AssetCounts returns the number of distinct assets each person appears in,
// keyed by person id. Persons with no assigned faces are absent from the map.
func (r *Repo) AssetCounts(ctx context.Context) (map[string]int64, error) {
	rows, err := r.q.ListPersonAssetCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("people: person asset counts: %w", err)
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		if row.PersonID != nil {
			out[*row.PersonID] = row.AssetCount
		}
	}
	return out, nil
}

// --- faces ------------------------------------------------------------------

// PutFace inserts a detected face. personID is empty for an unassigned face.
// contentHash records the asset's source bytes this face was detected from, so
// re-indexing unchanged media is a cache hit.
func (r *Repo) PutFace(ctx context.Context, f catalog.Face, contentHash string) error {
	if err := r.q.InsertFace(ctx, store.InsertFaceParams{
		ID:          f.ID,
		AssetID:     f.AssetID,
		PersonID:    nullablePersonID(f.PersonID),
		BboxX:       f.BBox[0],
		BboxY:       f.BBox[1],
		BboxW:       f.BBox[2],
		BboxH:       f.BBox[3],
		Confidence:  f.Confidence,
		ExtRef:      f.ExtRef,
		ContentHash: contentHash,
	}); err != nil {
		return fmt.Errorf("people: insert face %s: %w", f.ID, err)
	}
	return nil
}

// AssignFace assigns a face to a person (empty personID clears the assignment).
func (r *Repo) AssignFace(ctx context.Context, faceID, personID string) error {
	if err := r.q.AssignFacePerson(ctx, store.AssignFacePersonParams{
		PersonID: nullablePersonID(personID),
		ID:       faceID,
	}); err != nil {
		return fmt.Errorf("people: assign face %s: %w", faceID, err)
	}
	return nil
}

// GetFace loads a face by id, or ErrNotFound.
func (r *Repo) GetFace(ctx context.Context, id string) (catalog.Face, error) {
	row, err := r.q.GetFace(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.Face{}, fmt.Errorf("%w: face %s", ErrNotFound, id)
		}
		return catalog.Face{}, fmt.Errorf("people: get face %s: %w", id, err)
	}
	return faceFromGetRow(row), nil
}

// FacesForAsset returns every face detected in an asset.
func (r *Repo) FacesForAsset(ctx context.Context, assetID string) ([]catalog.Face, error) {
	rows, err := r.q.ListFacesForAsset(ctx, assetID)
	if err != nil {
		return nil, fmt.Errorf("people: faces for asset %s: %w", assetID, err)
	}
	out := make([]catalog.Face, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.Face{
			ID:         row.ID,
			AssetID:    row.AssetID,
			BBox:       [4]float64{row.BboxX, row.BboxY, row.BboxW, row.BboxH},
			PersonID:   derefPersonID(row.PersonID),
			Confidence: row.Confidence,
			ExtRef:     row.ExtRef,
		})
	}
	return out, nil
}

// AssetIDsForPerson returns the distinct asset ids containing a face assigned to
// the person, newest captures first.
func (r *Repo) AssetIDsForPerson(ctx context.Context, personID string) ([]string, error) {
	id := personID
	ids, err := r.q.ListAssetIDsForPerson(ctx, &id)
	if err != nil {
		return nil, fmt.Errorf("people: assets for person %s: %w", personID, err)
	}
	return ids, nil
}

// CountFacesForPerson returns how many faces are assigned to the person.
func (r *Repo) CountFacesForPerson(ctx context.Context, personID string) (int64, error) {
	id := personID
	n, err := r.q.CountFacesForPerson(ctx, &id)
	if err != nil {
		return 0, fmt.Errorf("people: count faces for person %s: %w", personID, err)
	}
	return n, nil
}

// PersonForExtRef returns the person id a previously-indexed face (identified by
// its backend ext_ref, e.g. a Rekognition FaceId) is assigned to. ok is false
// when the ref is unknown or assigned to no person yet.
func (r *Repo) PersonForExtRef(ctx context.Context, extRef string) (string, bool, error) {
	pid, err := r.q.PersonForExtRef(ctx, extRef)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("people: person for ext ref %s: %w", extRef, err)
	}
	if pid == nil || *pid == "" {
		return "", false, nil
	}
	return *pid, true, nil
}

// ListPersonNames returns a map of person id -> display name for the given ids
// (absent/unknown ids and unnamed clusters simply do not appear). The
// per-asset person set is tiny, so this resolves each id directly.
func (r *Repo) ListPersonNames(ctx context.Context, personIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(personIDs))
	for _, id := range personIDs {
		if id == "" {
			continue
		}
		p, err := r.GetPerson(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		if p.Name != "" {
			out[id] = p.Name
		}
	}
	return out, nil
}

// DeleteFacesForAsset removes all faces previously detected for an asset. The
// face-index pipeline calls this before re-detecting so a re-index does not
// accumulate duplicate face rows.
func (r *Repo) DeleteFacesForAsset(ctx context.Context, assetID string) error {
	if err := r.q.DeleteFacesForAsset(ctx, assetID); err != nil {
		return fmt.Errorf("people: delete faces for asset %s: %w", assetID, err)
	}
	return nil
}

// --- enrichment-run cache ledger --------------------------------------------

// HasRun reports whether an asset's exact bytes (contentHash) already had the
// given enrichment kind run. The enqueue path uses this to skip unchanged media.
func (r *Repo) HasRun(ctx context.Context, assetID, kind, contentHash string) (bool, error) {
	ok, err := r.q.HasEnrichmentRun(ctx, store.HasEnrichmentRunParams{
		AssetID:     assetID,
		Kind:        kind,
		ContentHash: contentHash,
	})
	if err != nil {
		return false, fmt.Errorf("people: has run %s/%s: %w", assetID, kind, err)
	}
	return ok, nil
}

// RecordRun marks that an enrichment kind ran over an asset's exact bytes.
func (r *Repo) RecordRun(ctx context.Context, assetID, kind, contentHash string) error {
	if err := r.q.RecordEnrichmentRun(ctx, store.RecordEnrichmentRunParams{
		AssetID:     assetID,
		Kind:        kind,
		ContentHash: contentHash,
	}); err != nil {
		return fmt.Errorf("people: record run %s/%s: %w", assetID, kind, err)
	}
	return nil
}

// --- row<->domain mapping ----------------------------------------------------

func faceFromGetRow(row store.GetFaceRow) catalog.Face {
	return catalog.Face{
		ID:         row.ID,
		AssetID:    row.AssetID,
		BBox:       [4]float64{row.BboxX, row.BboxY, row.BboxW, row.BboxH},
		PersonID:   derefPersonID(row.PersonID),
		Confidence: row.Confidence,
		ExtRef:     row.ExtRef,
	}
}

// nullablePersonID maps the empty string ("" = unassigned, per catalog.Face) to a
// SQL NULL so the faces.person_id FK is genuinely null rather than a "" that
// would dangle against the persons table.
func nullablePersonID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// derefPersonID maps a nullable person id back to catalog.Face's "" convention.
func derefPersonID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}
