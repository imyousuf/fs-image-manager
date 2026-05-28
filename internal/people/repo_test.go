package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/people"
)

func TestRepoPersonRoundTrip(t *testing.T) {
	repo, _, _ := newRepo(t)
	ctx := context.Background()

	if err := repo.PutPerson(ctx, catalog.Person{ID: "p1", Name: "Alice"}, "coll"); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}
	got, err := repo.GetPerson(ctx, "p1")
	if err != nil {
		t.Fatalf("GetPerson: %v", err)
	}
	if got.Name != "Alice" {
		t.Fatalf("name = %q, want Alice", got.Name)
	}

	if _, err := repo.GetPerson(ctx, "missing"); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("GetPerson(missing) err = %v, want ErrNotFound", err)
	}
}

func TestRepoRenameNotFound(t *testing.T) {
	repo, _, _ := newRepo(t)
	if err := repo.RenamePerson(context.Background(), "nope", "X"); !errors.Is(err, people.ErrNotFound) {
		t.Fatalf("RenamePerson(missing) err = %v, want ErrNotFound", err)
	}
}

// TestRepoRenameDuplicateNameRejected confirms the partial-unique index on
// persons.name surfaces as an error when two clusters are given the same name,
// while multiple UNNAMED clusters are allowed to coexist.
func TestRepoRenameDuplicateNameRejected(t *testing.T) {
	repo, _, _ := newRepo(t)
	ctx := context.Background()
	// Two unknown clusters coexist (empty names are exempt from the unique index).
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p1"}, "coll"); err != nil {
		t.Fatalf("PutPerson p1: %v", err)
	}
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p2"}, "coll"); err != nil {
		t.Fatalf("PutPerson p2 (second unknown) should be allowed: %v", err)
	}
	if err := repo.RenamePerson(ctx, "p1", "Alice"); err != nil {
		t.Fatalf("RenamePerson p1: %v", err)
	}
	if err := repo.RenamePerson(ctx, "p2", "Alice"); err == nil {
		t.Fatalf("RenamePerson p2 to a taken name should fail")
	}
}

func TestRepoFacePersonForExtRef(t *testing.T) {
	repo, cat, _ := newRepo(t)
	ctx := context.Background()
	seedAsset(t, cat, "a1")
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p1", Name: "Alice"}, "coll"); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}
	face := catalog.Face{ID: "f1", AssetID: "a1", ExtRef: "F1", PersonID: "p1", Confidence: 99}
	if err := repo.PutFace(ctx, face, "hash1"); err != nil {
		t.Fatalf("PutFace: %v", err)
	}

	pid, ok, err := repo.PersonForExtRef(ctx, "F1")
	if err != nil || !ok || pid != "p1" {
		t.Fatalf("PersonForExtRef(F1) = (%q,%v,%v), want (p1,true,nil)", pid, ok, err)
	}
	if _, ok, _ := repo.PersonForExtRef(ctx, "unknown"); ok {
		t.Fatalf("PersonForExtRef(unknown) should be ok=false")
	}
}

// TestRepoFacePersonForExtRefUnassigned confirms a face indexed but not yet
// assigned to a person does not resolve (ok=false), so the pipeline does not
// merge into an unassigned face's (non-existent) cluster.
func TestRepoFacePersonForExtRefUnassigned(t *testing.T) {
	repo, cat, _ := newRepo(t)
	ctx := context.Background()
	seedAsset(t, cat, "a1")
	if err := repo.PutFace(ctx, catalog.Face{ID: "f1", AssetID: "a1", ExtRef: "F1"}, "h"); err != nil {
		t.Fatalf("PutFace: %v", err)
	}
	if _, ok, _ := repo.PersonForExtRef(ctx, "F1"); ok {
		t.Fatalf("unassigned face should not resolve a person")
	}
}

func TestRepoEnrichmentRunLedger(t *testing.T) {
	repo, cat, _ := newRepo(t)
	ctx := context.Background()
	seedAsset(t, cat, "a1")

	ok, err := repo.HasRun(ctx, "a1", catalog.JobKindFaceIndex, "h1")
	if err != nil || ok {
		t.Fatalf("HasRun before record = (%v,%v), want (false,nil)", ok, err)
	}
	if err := repo.RecordRun(ctx, "a1", catalog.JobKindFaceIndex, "h1"); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	ok, err = repo.HasRun(ctx, "a1", catalog.JobKindFaceIndex, "h1")
	if err != nil || !ok {
		t.Fatalf("HasRun after record = (%v,%v), want (true,nil)", ok, err)
	}
	// A different hash (changed bytes) is not yet run.
	if ok, _ := repo.HasRun(ctx, "a1", catalog.JobKindFaceIndex, "h2"); ok {
		t.Fatalf("HasRun for new hash should be false")
	}
}

// TestRepoFacesCascadeOnAssetDelete confirms deleting an asset cascades to its
// faces (the FK), so a removed photo leaves no dangling face rows.
func TestRepoFacesCascadeOnAssetDelete(t *testing.T) {
	repo, cat, _ := newRepo(t)
	ctx := context.Background()
	seedAsset(t, cat, "a1")
	if err := repo.PutFace(ctx, catalog.Face{ID: "f1", AssetID: "a1", ExtRef: "F1"}, "h"); err != nil {
		t.Fatalf("PutFace: %v", err)
	}
	if err := cat.DeleteAsset(ctx, "a1"); err != nil {
		t.Fatalf("DeleteAsset: %v", err)
	}
	faces, err := repo.FacesForAsset(ctx, "a1")
	if err != nil {
		t.Fatalf("FacesForAsset: %v", err)
	}
	if len(faces) != 0 {
		t.Fatalf("faces should cascade-delete with the asset, got %d", len(faces))
	}
}
