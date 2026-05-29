package people

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/httpx"
)

// AssetViewer turns ordered asset ids into the public AssetView JSON items, the
// same shape folder-browse and search return. It is injected (rather than people
// depending on internal/media) so "photos of X" emits identical items to the
// rest of the API. serve wires it to a closure over the catalog repo + media's
// view builder (the same assetViewer search uses). Missing ids are dropped.
type AssetViewer interface {
	ViewsForIDs(ctx context.Context, ids []string) ([]any, error)
}

// peopleAPI is the slice of the repo the handlers read/write. *Repo satisfies it.
type peopleAPI interface {
	ListPersons(ctx context.Context) ([]catalog.Person, error)
	AssetCounts(ctx context.Context) (map[string]int64, error)
	AssetIDsForPerson(ctx context.Context, personID string) ([]string, error)
	RenamePerson(ctx context.Context, id, name string) error
}

// Handlers serves the public People API (docs/specs/_contracts.md section 5):
//
//	GET  /people                 -> [PersonView]
//	GET  /people/{id}/assets     -> {items:[AssetView], next}
//	POST /people/{id}            -> rename a cluster ({name})
//
// Paths are registered WITHOUT the "/api" prefix (the server strips it), matching
// search.Handlers.
type Handlers struct {
	repo   peopleAPI
	viewer AssetViewer
}

// NewHandlers builds the People HTTP handlers over the repo and an AssetViewer.
func NewHandlers(repo peopleAPI, viewer AssetViewer) *Handlers {
	return &Handlers{repo: repo, viewer: viewer}
}

// Register mounts the People routes on the public API mux.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /people", h.list)
	mux.HandleFunc("GET /people/{id}/assets", h.assets)
	mux.HandleFunc("POST /people/{id}", h.rename)
}

// PersonView is the wire shape of a person in the People view: the cluster id,
// its name ("" = unknown/pending), the cover face id for a thumbnail, and how
// many photos the person appears in.
type PersonView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CoverFaceID string `json:"coverFaceId"`
	AssetCount  int64  `json:"assetCount"`
}

// list handles GET /people -> [PersonView], named clusters first then unknown.
func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	persons, err := h.repo.ListPersons(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	counts, err := h.repo.AssetCounts(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]PersonView, 0, len(persons))
	for _, p := range persons {
		out = append(out, PersonView{
			ID:          p.ID,
			Name:        p.Name,
			CoverFaceID: p.CoverFaceID,
			AssetCount:  counts[p.ID],
		})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// assetsResponse mirrors the browse/search envelope so the frontend reuses one
// renderer. People assets are returned in one page (a person's photo count is
// modest); a cursor field is kept for shape-compatibility and future paging.
type assetsResponse struct {
	Items []any  `json:"items"`
	Next  string `json:"next"`
}

// assets handles GET /people/{id}/assets -> {items:[AssetView], next}.
func (h *Handlers) assets(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "person id is required")
		return
	}
	ids, err := h.repo.AssetIDsForPerson(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	items, err := h.viewer.ViewsForIDs(r.Context(), ids)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if items == nil {
		items = []any{}
	}
	httpx.WriteJSON(w, http.StatusOK, assetsResponse{Items: items, Next: ""})
}

// renameRequest is the body of POST /people/{id}.
type renameRequest struct {
	Name string `json:"name"`
}

// rename handles POST /people/{id} {name}: label (or relabel) a face cluster.
// An empty name is rejected (use a dedicated "clear" path if ever needed); a
// duplicate name (another cluster already owns it) surfaces as a conflict.
func (h *Handlers) rename(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "person id is required")
		return
	}
	var req renameRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_name", "name must not be empty")
		return
	}
	if err := h.repo.RenamePerson(r.Context(), id, name); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "person_not_found", err.Error())
		case isUniqueViolation(err):
			httpx.WriteError(w, http.StatusConflict, "name_taken", "another person already has that name")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PersonView{ID: id, Name: name})
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure
// (the partial unique index on persons.name), surfaced as a 409 on rename.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
