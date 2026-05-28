package media

import (
	"net/http"
	"strconv"

	"github.com/imyousuf/fs-image-manager/internal/httpx"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// listLibraries handles GET /api/libraries -> [{alias,name}].
func (h *Handlers) listLibraries(w http.ResponseWriter, _ *http.Request) {
	libs := h.resolver.Libraries()
	out := make([]LibraryView, 0, len(libs))
	for _, l := range libs {
		out = append(out, LibraryView{Alias: l.Alias, Name: l.Name})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// browse handles GET /api/assets?path=<alias>/<dir>&cursor=&limit= — a folder
// listing of one logical asset per row, plus the immediate child folders. The
// path may be just "<alias>" for a library root. All path input is validated
// through the resolver (traversal-proof).
func (h *Handlers) browse(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("path")
	if raw == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "missing path query parameter")
		return
	}
	// Validate against the resolver (rejects unknown alias / traversal), then
	// split into alias + library-relative dir.
	if _, err := httpx.ParseMediaPath(h.resolver, raw); err != nil {
		httpx.WriteAPIError(w, err)
		return
	}
	alias, dir, err := mediapath.Split(mediapath.MediaPath(raw))
	if err != nil {
		httpx.WriteAPIError(w, err)
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := parseLimit(r.URL.Query().Get("limit"))

	page, err := h.repo.ListByDir(r.Context(), alias, dir, cursor, limit)
	if err != nil {
		httpx.WriteAPIError(w, err)
		return
	}

	items := make([]AssetView, 0, len(page.Assets))
	for _, a := range page.Assets {
		items = append(items, ToView(a))
	}
	httpx.WriteJSON(w, http.StatusOK, browseResponse{
		Items: items,
		Next:  page.NextCursor,
		Dirs:  page.Dirs,
	})
}

// parseLimit parses the limit query param, falling back to 0 (repo default) on
// blank/invalid input. Negative values clamp to 0.
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
