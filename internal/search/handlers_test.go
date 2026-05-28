package search_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/search"
)

// idViewer renders each asset id as a tiny {"id":...} object, enough to assert
// the envelope shape and ordering without pulling in internal/media.
type idViewer struct{}

func (idViewer) ViewsForIDs(_ context.Context, ids []string) ([]any, error) {
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]string{"id": id})
	}
	return out, nil
}

func serveTest(t *testing.T, ix *search.Index) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	search.NewHandlers(ix, idViewer{}).Register(mux)
	return mux
}

func TestSearchHandler(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	h := serveTest(t, ix)

	req := httptest.NewRequest(http.MethodGet, "/assets/search?q=beach", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]string `json:"items"`
		Next  string              `json:"next"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	got := map[string]bool{}
	for _, it := range resp.Items {
		got[it["id"]] = true
	}
	if !got["p1"] || !got["v1"] {
		t.Fatalf("beach search items = %v, want p1+v1", resp.Items)
	}
}

func TestSearchHandlerDateFilter(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	h := serveTest(t, ix)

	// Bare YYYY-MM-DD bounds; inclusive of the whole 2021-02 range.
	req := httptest.NewRequest(http.MethodGet, "/assets/search?from=2021-02-01&to=2021-02-28", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]string `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 3 { // p1,p2,v1
		t.Fatalf("date filter items = %d, want 3 (%s)", len(resp.Items), rec.Body.String())
	}
}

func TestSearchHandlerBadCursor(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	h := serveTest(t, ix)

	req := httptest.NewRequest(http.MethodGet, "/assets/search?cursor=xyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestTimelineHandler(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)
	h := serveTest(t, ix)

	req := httptest.NewRequest(http.MethodGet, "/assets/timeline", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var buckets []search.Bucket
	if err := json.Unmarshal(rec.Body.Bytes(), &buckets); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("timeline buckets = %d, want 3 (%s)", len(buckets), rec.Body.String())
	}
}
