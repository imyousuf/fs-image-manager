package people_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/people"
)

// stubViewer renders asset ids into trivial view items, standing in for the
// media.ToView-backed viewer serve wires in production.
type stubViewer struct{}

func (stubViewer) ViewsForIDs(_ context.Context, ids []string) ([]any, error) {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"id": id})
	}
	return out, nil
}

// newServer mounts the People handlers over a fresh repo and returns the test
// server plus the repo for seeding.
func newServer(t *testing.T) (*httptest.Server, *people.Repo, *catalog.Repo) {
	t.Helper()
	repo, cat, _ := newRepo(t)
	mux := http.NewServeMux()
	people.NewHandlers(repo, stubViewer{}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, repo, cat
}

func TestHandlersListPeople(t *testing.T) {
	srv, repo, cat := newServer(t)
	ctx := context.Background()
	seedAsset(t, cat, "a1")
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p1", Name: "Alice", CoverFaceID: "f1"}, "coll"); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}
	if err := repo.PutFace(ctx, catalog.Face{ID: "f1", AssetID: "a1", ExtRef: "F1", PersonID: "p1"}, "h"); err != nil {
		t.Fatalf("PutFace: %v", err)
	}

	resp, err := http.Get(srv.URL + "/people")
	if err != nil {
		t.Fatalf("GET /people: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got []people.PersonView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Alice" || got[0].AssetCount != 1 {
		t.Fatalf("people = %+v, want one Alice with assetCount 1", got)
	}
}

func TestHandlersPersonAssets(t *testing.T) {
	srv, repo, cat := newServer(t)
	ctx := context.Background()
	seedAsset(t, cat, "a1")
	seedAsset(t, cat, "a2")
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p1", Name: "Alice"}, "coll"); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}
	for i, id := range []string{"a1", "a2"} {
		fid := "f" + string(rune('1'+i))
		if err := repo.PutFace(ctx, catalog.Face{ID: fid, AssetID: id, ExtRef: "F" + fid, PersonID: "p1"}, "h"); err != nil {
			t.Fatalf("PutFace %s: %v", fid, err)
		}
	}

	resp, err := http.Get(srv.URL + "/people/p1/assets")
	if err != nil {
		t.Fatalf("GET assets: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("expected 2 asset items, got %d (%+v)", len(body.Items), body.Items)
	}
}

func TestHandlersRename(t *testing.T) {
	srv, repo, _ := newServer(t)
	ctx := context.Background()
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p1"}, "coll"); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}

	resp, err := http.Post(srv.URL+"/people/p1", "application/json", strings.NewReader(`{"name":"Bob"}`))
	if err != nil {
		t.Fatalf("POST rename: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got, err := repo.GetPerson(ctx, "p1")
	if err != nil {
		t.Fatalf("GetPerson: %v", err)
	}
	if got.Name != "Bob" {
		t.Fatalf("name = %q, want Bob", got.Name)
	}
}

func TestHandlersRenameEmptyRejected(t *testing.T) {
	srv, repo, _ := newServer(t)
	if err := repo.PutPerson(context.Background(), catalog.Person{ID: "p1"}, "coll"); err != nil {
		t.Fatalf("PutPerson: %v", err)
	}
	resp, err := http.Post(srv.URL+"/people/p1", "application/json", strings.NewReader(`{"name":"  "}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty name status = %d, want 400", resp.StatusCode)
	}
}

func TestHandlersRenameNotFound(t *testing.T) {
	srv, _, _ := newServer(t)
	resp, err := http.Post(srv.URL+"/people/missing", "application/json", strings.NewReader(`{"name":"X"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing person rename status = %d, want 404", resp.StatusCode)
	}
}

func TestHandlersRenameDuplicateConflict(t *testing.T) {
	srv, repo, _ := newServer(t)
	ctx := context.Background()
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p1", Name: "Alice"}, "coll"); err != nil {
		t.Fatalf("PutPerson p1: %v", err)
	}
	if err := repo.PutPerson(ctx, catalog.Person{ID: "p2"}, "coll"); err != nil {
		t.Fatalf("PutPerson p2: %v", err)
	}
	resp, err := http.Post(srv.URL+"/people/p2", "application/json", strings.NewReader(`{"name":"Alice"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate name status = %d, want 409", resp.StatusCode)
	}
}
