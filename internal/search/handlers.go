package search

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/httpx"
)

// AssetViewer turns ordered asset ids into the JSON view items the public API
// returns. It is injected (rather than search depending on internal/media) so
// the search endpoint emits exactly the same AssetView shape as folder browse
// without search owning that struct: serve wires it to a closure over the
// catalog repo + media's view builder. Order in == order out; ids that no longer
// exist are dropped.
type AssetViewer interface {
	ViewsForIDs(ctx context.Context, ids []string) ([]any, error)
}

// Handlers serves the public search + timeline API (docs/specs/_contracts.md
// sec.5). It reads the search Index for hit ids and the injected AssetViewer to
// render them as AssetViews.
type Handlers struct {
	ix     *Index
	viewer AssetViewer
}

// NewHandlers builds the search HTTP handlers over an Index and an AssetViewer.
func NewHandlers(ix *Index, viewer AssetViewer) *Handlers {
	return &Handlers{ix: ix, viewer: viewer}
}

// Register mounts the search routes on the public API mux. Paths are registered
// WITHOUT the "/api" prefix (the server strips it).
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /assets/search", h.search)
	mux.HandleFunc("GET /assets/timeline", h.timeline)
}

// searchResponse mirrors the browse envelope {items:[AssetView], next} so the
// frontend can reuse one renderer for folder browse and search.
type searchResponse struct {
	Items []any  `json:"items"`
	Next  string `json:"next"`
}

// search handles GET /assets/search?q=&from=&to=&alias=&camera=&person=&cursor=&limit=.
func (h *Handlers) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseDate(q.Get("from"), false)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_from", err.Error())
		return
	}
	to, err := parseDate(q.Get("to"), true)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_to", err.Error())
		return
	}

	query := Query{
		Text:   q.Get("q"),
		From:   from,
		To:     to,
		Alias:  q.Get("alias"),
		Camera: q.Get("camera"),
		Person: q.Get("person"),
		Cursor: q.Get("cursor"),
		Limit:  parseLimit(q.Get("limit")),
	}

	res, err := h.ix.Search(r.Context(), query)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_search", err.Error())
		return
	}

	items, err := h.viewer.ViewsForIDs(r.Context(), res.AssetIDs)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if items == nil {
		items = []any{}
	}
	httpx.WriteJSON(w, http.StatusOK, searchResponse{Items: items, Next: res.NextCursor})
}

// timeline handles GET /assets/timeline?alias=&from=&to= -> [{date,count}].
func (h *Handlers) timeline(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseDate(q.Get("from"), false)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_from", err.Error())
		return
	}
	to, err := parseDate(q.Get("to"), true)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_to", err.Error())
		return
	}

	buckets, err := h.ix.Timeline(r.Context(), TimelineQuery{
		Alias: q.Get("alias"),
		From:  from,
		To:    to,
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, buckets)
}

// parseDate parses a from/to query value. It accepts a full RFC3339 timestamp or
// a bare "YYYY-MM-DD" date. For a bare date, the lower bound is the start of the
// day and the upper bound (endOfDay) is the last instant of the day, so an
// inclusive [from,to] range over whole days works as a user expects. Empty -> zero.
func parseDate(s string, endOfDay bool) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	d, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		return d.Add(24*time.Hour - time.Nanosecond), nil
	}
	return d, nil
}

// parseLimit parses the limit query param; a missing/invalid value yields 0,
// which the Index treats as DefaultLimit.
func parseLimit(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
