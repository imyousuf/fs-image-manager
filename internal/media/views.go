package media

import (
	"net/url"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// AssetView is the JSON shape the browse/search APIs return for one asset
// (docs/specs/_contracts.md §5). URLs are absolute-from-root API paths the SPA
// can use directly.
type AssetView struct {
	ID           string     `json:"id"`
	Alias        string     `json:"alias"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	NeedsDevelop bool       `json:"needsDevelop"`
	DisplayThumb string     `json:"displayThumb"`
	Preview      string     `json:"preview"`
	Stream       string     `json:"stream"`
	Download     string     `json:"download"`
	Files        []FileView `json:"files"`
	CapturedAt   *time.Time `json:"capturedAt,omitempty"`
}

// FileView is the per-file entry inside an AssetView.
type FileView struct {
	MediaPath string `json:"mediaPath"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
}

// LibraryView is the JSON shape for GET /api/libraries.
type LibraryView struct {
	Alias string `json:"alias"`
	Name  string `json:"name"`
}

// browseResponse is the envelope for GET /api/assets.
type browseResponse struct {
	Items []AssetView `json:"items"`
	Next  string      `json:"next"`
	Dirs  []string    `json:"dirs"`
}

// ToView converts a catalog.Asset to its public API representation (AssetView).
// The asset id is URL-path-safe (hex), so the per-asset URLs need no escaping;
// the human name is the base name. Exported so other packages that surface the
// same asset shape (e.g. search-index's result handler mapping ids -> views)
// reuse one canonical AssetView builder instead of duplicating URL/field rules.
func ToView(a catalog.Asset) AssetView {
	files := make([]FileView, 0, len(a.Files))
	for _, f := range a.Files {
		files = append(files, FileView{
			MediaPath: string(f.MediaPath),
			Kind:      string(f.Kind),
			Size:      f.Size,
		})
	}
	base := "/api/assets/" + url.PathEscape(a.ID)
	return AssetView{
		ID:           a.ID,
		Alias:        a.Alias,
		Name:         a.BaseName,
		Kind:         a.Kind,
		NeedsDevelop: catalog.NeedsDevelop(a),
		DisplayThumb: base + "/thumb",
		Preview:      base + "/preview",
		Stream:       base + "/stream",
		Download:     base + "/download",
		Files:        files,
		CapturedAt:   a.CapturedAt,
	}
}
