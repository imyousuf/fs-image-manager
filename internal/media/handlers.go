package media

import (
	"context"
	"net/http"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// Handlers serves the public media API (docs/specs/_contracts.md §5): library
// list, folder browse, per-asset thumb/preview/stream/download, multi-select
// zip download and upload. It reads the catalog, generates+caches derivatives
// on demand, and streams originals through the traversal-proof resolver.
//
// A nil Queue is tolerated end-to-end: browsing and host thumbnails/posters
// work with no worker; RAW-only assets and videos lacking ffmpeg serve a
// placeholder.
type Handlers struct {
	resolver mediapath.Resolver
	repo     *catalog.Repo
	cache    catalog.Cache
	onUpload func(ctx context.Context, alias string) // optional ingest trigger
}

// HandlerOptions bundles the dependencies for NewHandlers. Job enqueueing for
// new assets is the ingester's responsibility (not the request path), so the
// Queue is not needed here — browsing/serving never blocks on a worker.
type HandlerOptions struct {
	Resolver mediapath.Resolver
	Repo     *catalog.Repo
	Cache    catalog.Cache
	// OnUpload, if set, is invoked after a successful upload with the target
	// library alias so the new file can be ingested synchronously (so it shows
	// up in listings immediately). serve wires this to the ingester.
	OnUpload func(ctx context.Context, alias string)
}

// NewHandlers builds the media HTTP handlers.
func NewHandlers(o HandlerOptions) *Handlers {
	return &Handlers{
		resolver: o.Resolver,
		repo:     o.Repo,
		cache:    o.Cache,
		onUpload: o.OnUpload,
	}
}

// Register mounts the media routes on the public API mux. Paths are registered
// WITHOUT the "/api" prefix (the server strips it). Method+pattern syntax
// (Go 1.22+) gives us path params via r.PathValue.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /libraries", h.listLibraries)
	mux.HandleFunc("GET /assets", h.browse)
	mux.HandleFunc("GET /assets/{id}/thumb", h.thumb)
	mux.HandleFunc("GET /assets/{id}/preview", h.preview)
	mux.HandleFunc("GET /assets/{id}/stream", h.stream)
	mux.HandleFunc("GET /assets/{id}/download", h.downloadAsset)
	mux.HandleFunc("POST /download", h.downloadZip)
	mux.HandleFunc("POST /upload", h.upload)
}
