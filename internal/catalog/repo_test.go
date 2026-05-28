package catalog_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/media"
)

// newRepo opens a fresh migrated sqlite db in a temp dir and returns a repo.
func newRepo(t *testing.T) *catalog.Repo {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "fsim.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return catalog.NewRepo(conn, media.ClassifyExt)
}

func mustPut(t *testing.T, r *catalog.Repo, inputs ...catalog.FileInput) []catalog.Asset {
	t.Helper()
	assets := catalog.Group(inputs, media.ClassifyExt)
	for _, a := range assets {
		if err := r.PutAsset(context.Background(), a); err != nil {
			t.Fatalf("PutAsset: %v", err)
		}
	}
	return assets
}

func TestRepoPutGetAsset(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	assets := mustPut(t, r,
		in("pictures", "2021", "IMG_1234.CR3"),
		in("pictures", "2021", "IMG_1234.JPG"),
		in("pictures", "2021", "IMG_1234.xmp"),
	)
	got, err := r.GetAsset(ctx, assets[0].ID)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got.DisplayPath != "pictures/2021/IMG_1234.JPG" {
		t.Errorf("DisplayPath = %q", got.DisplayPath)
	}
	if len(got.Files) != 3 {
		t.Errorf("files = %d, want 3", len(got.Files))
	}
	if catalog.NeedsDevelop(got) {
		t.Error("RAW+JPG asset persisted as needs-develop")
	}
}

func TestRepoListByDirPagination(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	// 5 image assets in pictures/a.
	for _, n := range []string{"A.JPG", "B.JPG", "C.JPG", "D.JPG", "E.JPG"} {
		mustPut(t, r, in("pictures", "a", n))
	}
	page, err := r.ListByDir(ctx, "pictures", "a", "", 2)
	if err != nil {
		t.Fatalf("ListByDir: %v", err)
	}
	if len(page.Assets) != 2 || page.NextCursor == "" {
		t.Fatalf("first page = %d assets, next=%q; want 2 + cursor", len(page.Assets), page.NextCursor)
	}
	if page.Assets[0].BaseName != "A" || page.Assets[1].BaseName != "B" {
		t.Errorf("page order = %q,%q; want A,B", page.Assets[0].BaseName, page.Assets[1].BaseName)
	}
	// Walk the cursor to the end.
	seen := 2
	cursor := page.NextCursor
	for cursor != "" {
		p, err := r.ListByDir(ctx, "pictures", "a", cursor, 2)
		if err != nil {
			t.Fatalf("ListByDir page: %v", err)
		}
		seen += len(p.Assets)
		cursor = p.NextCursor
	}
	if seen != 5 {
		t.Errorf("paged through %d assets, want 5", seen)
	}
}

func TestRepoChildDirs(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	mustPut(t, r, in("pictures", "", "root.JPG"))
	mustPut(t, r, in("pictures", "2020", "a.JPG"))
	mustPut(t, r, in("pictures", "2021", "b.JPG"))
	mustPut(t, r, in("pictures", "2021/sub", "c.JPG"))

	// Root view: immediate children are 2020 and 2021 (not 2021/sub).
	page, err := r.ListByDir(ctx, "pictures", "", "", 0)
	if err != nil {
		t.Fatalf("ListByDir root: %v", err)
	}
	if got := page.Dirs; len(got) != 2 || got[0] != "2020" || got[1] != "2021" {
		t.Errorf("root child dirs = %v, want [2020 2021]", got)
	}
	if len(page.Assets) != 1 || page.Assets[0].BaseName != "root" {
		t.Errorf("root assets = %+v, want just root", page.Assets)
	}
	// 2021 view: child is "sub".
	p2, err := r.ListByDir(ctx, "pictures", "2021", "", 0)
	if err != nil {
		t.Fatalf("ListByDir 2021: %v", err)
	}
	if got := p2.Dirs; len(got) != 1 || got[0] != "sub" {
		t.Errorf("2021 child dirs = %v, want [sub]", got)
	}
}

func TestRepoDeleteFilePrunesEmptyAsset(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	assets := mustPut(t, r,
		in("pictures", "d", "IMG.CR3"),
		in("pictures", "d", "IMG.JPG"),
	)
	id := assets[0].ID

	// Delete the JPG: asset survives (CR3 remains) and now needs develop.
	pruned, owner, err := r.DeleteFile(ctx, "pictures/d/IMG.JPG")
	if err != nil {
		t.Fatalf("DeleteFile JPG: %v", err)
	}
	if pruned || owner != id {
		t.Errorf("deleting JPG: pruned=%v owner=%q, want false + %q", pruned, owner, id)
	}
	// Re-group is the ingester's job; here the asset row still has the JPG as
	// display until a regroup runs. We only assert the file row is gone.
	if _, err := r.GetAsset(ctx, id); err != nil {
		t.Fatalf("asset should still exist after one file removed: %v", err)
	}

	// Delete the CR3 too: asset is now empty and pruned.
	pruned, owner, err = r.DeleteFile(ctx, "pictures/d/IMG.CR3")
	if err != nil {
		t.Fatalf("DeleteFile CR3: %v", err)
	}
	if !pruned || owner != id {
		t.Errorf("deleting last file: pruned=%v owner=%q, want true + %q", pruned, owner, id)
	}
	if _, err := r.GetAsset(ctx, id); err == nil {
		t.Error("asset should be gone after its last file was deleted")
	}
}

func TestRepoSnapshotAndCapturedAt(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	assets := mustPut(t, r, in("pictures", "s", "X.JPG"))

	snap, err := r.SnapshotFiles(ctx, "pictures")
	if err != nil {
		t.Fatalf("SnapshotFiles: %v", err)
	}
	cf, ok := snap["pictures/s/X.JPG"]
	if !ok {
		t.Fatalf("snapshot missing the file: %v", snap)
	}
	if cf.Kind != catalog.FileKindJPG || cf.Size != 1234 {
		t.Errorf("snapshot entry = %+v", cf)
	}

	when := time.Date(2021, 3, 5, 12, 0, 0, 0, time.UTC)
	if err := r.SetCapturedAt(ctx, assets[0].ID, when); err != nil {
		t.Fatalf("SetCapturedAt: %v", err)
	}
	got, _ := r.GetAsset(ctx, assets[0].ID)
	if got.CapturedAt == nil || !got.CapturedAt.Equal(when) {
		t.Errorf("CapturedAt = %v, want %v", got.CapturedAt, when)
	}
}

func TestRepoDerivativeRoundTrip(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	assets := mustPut(t, r, in("pictures", "d", "X.JPG"))
	id := assets[0].ID

	d := catalog.Derivative{AssetID: id, Kind: "thumb", Params: "w=320", Path: "/cache/ab/xyz", Mime: "image/jpeg"}
	if err := r.PutDerivative(ctx, d, "srchash"); err != nil {
		t.Fatalf("PutDerivative: %v", err)
	}
	got, err := r.GetDerivative(ctx, id, "thumb", "w=320")
	if err != nil {
		t.Fatalf("GetDerivative: %v", err)
	}
	if got.Path != "/cache/ab/xyz" || got.Mime != "image/jpeg" {
		t.Errorf("derivative = %+v", got)
	}
	if _, err := r.GetDerivative(ctx, id, "preview", ""); err == nil {
		t.Error("missing derivative should error")
	}
}
