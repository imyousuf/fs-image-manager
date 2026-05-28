package media

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/imyousuf/fs-image-manager/internal/cache"
	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/httpx"
)

// thumb handles GET /api/assets/{id}/thumb?size=320. It returns a cached JPEG
// thumbnail (poster for video), generating it on a miss. RAW-only and
// ffmpeg-less video assets get a placeholder so the grid always renders.
func (h *Handlers) thumb(w http.ResponseWriter, r *http.Request) {
	h.serveDerivative(w, r, "thumb", clampSize(r.URL.Query().Get("size"), DefaultThumbSize))
}

// preview handles GET /api/assets/{id}/preview — a display-size derivative.
func (h *Handlers) preview(w http.ResponseWriter, r *http.Request) {
	h.serveDerivative(w, r, "preview", clampSize(r.URL.Query().Get("size"), DefaultPreviewSize))
}

// serveDerivative is the shared thumb/preview path: look up the asset, compute
// a content-addressed cache key, serve a hit, or generate+cache+serve a miss.
func (h *Handlers) serveDerivative(w http.ResponseWriter, r *http.Request, kind string, size int) {
	id := r.PathValue("id")
	asset, err := h.repo.GetAsset(r.Context(), id)
	if err != nil {
		h.writeAssetErr(w, err)
		return
	}

	srcHash := displaySourceHash(asset)
	params := "w=" + strconv.Itoa(size)
	key := h.cache.Key(asset.ID, kind, params, srcHash)

	if path, ok := h.cache.Get(key); ok {
		h.serveCachedFile(w, r, path, MimeJPEG)
		return
	}

	data, mime, err := h.generate(r.Context(), asset, kind, size)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "derive_failed", err.Error())
		return
	}
	if _, perr := h.cache.Put(key, bytes.NewReader(data), mime); perr != nil {
		// Caching is best-effort; still serve the bytes.
		_ = perr
	} else {
		_ = h.repo.PutDerivative(r.Context(), catalog.Derivative{
			AssetID: asset.ID, Kind: kind, Params: params, Mime: mime,
		}, srcHash)
	}
	cache.ServeBytes(w, r, data, mime)
}

// generate produces the derivative bytes for an asset on a cache miss. For an
// image asset it decodes the JPG/PNG display source on the host. For a video it
// grabs a poster via ffmpeg. RAW-only images and ffmpeg-less videos return a
// placeholder (never an error) so the UI always has something to show.
func (h *Handlers) generate(ctx context.Context, asset catalog.Asset, kind string, size int) ([]byte, string, error) {
	if asset.Kind == catalog.AssetKindVideo {
		return h.generateVideoPoster(ctx, asset, size)
	}
	// Image asset.
	if catalog.NeedsDevelop(asset) {
		// RAW-only: no host display source until the develop-raw job lands.
		return Placeholder(PlaceholderRAW, size), MimeJPEG, nil
	}
	displayFile, ok := displayMember(asset)
	if !ok || !IsHostDecodable(displayFile.Kind) {
		// Non-decodable image (e.g. heic) — placeholder until a worker converts.
		return Placeholder(placeholderOther, size), MimeJPEG, nil
	}
	f, err := h.resolver.Open(asset.DisplayPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Placeholder(placeholderOther, size), MimeJPEG, nil
		}
		return nil, "", err
	}
	defer func() { _ = f.Close() }()

	var data []byte
	if kind == "preview" {
		data, err = Preview(f, size)
	} else {
		data, err = Thumbnail(f, size)
	}
	if err != nil {
		return nil, "", err
	}
	return data, MimeJPEG, nil
}

// generateVideoPoster grabs a poster frame via ffmpeg, or a placeholder if
// ffmpeg is unavailable / the grab fails.
func (h *Handlers) generateVideoPoster(ctx context.Context, asset catalog.Asset, size int) ([]byte, string, error) {
	abs, _, _, err := h.resolver.Resolve(asset.DisplayPath)
	if err != nil {
		return Placeholder(PlaceholderVideo, size), MimeJPEG, nil //nolint:nilerr // degrade to placeholder
	}
	poster, err := VideoPoster(ctx, abs, size)
	if err != nil {
		return Placeholder(PlaceholderVideo, size), MimeJPEG, nil //nolint:nilerr // ffmpeg absent or failed
	}
	return poster, MimeJPEG, nil
}

// displayMember returns the asset's display File (the one DisplayPath points
// at), and whether it was found.
func displayMember(a catalog.Asset) (catalog.File, bool) {
	for _, f := range a.Files {
		if f.MediaPath == a.DisplayPath {
			return f, true
		}
	}
	return catalog.File{}, false
}

// displaySourceHash returns the content hash of the asset's display source, used
// to key derivatives so a changed source invalidates them. Falls back to the
// asset id when the hash is unknown (pre-hash assets).
func displaySourceHash(a catalog.Asset) string {
	if f, ok := displayMember(a); ok && f.Hash != "" {
		return f.Hash
	}
	return a.ID
}

// clampSize parses a size query param, bounding it to a sane range; blank or
// invalid falls back to def.
func clampSize(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	const maxSize = 4096
	if n > maxSize {
		return maxSize
	}
	return n
}

// serveCachedFile serves a derivative already on disk with ETag/Cache-Control.
// It reads the file into memory (derivatives are small) and uses the byte
// server so conditional requests are honoured uniformly.
func (h *Handlers) serveCachedFile(w http.ResponseWriter, r *http.Request, path, mime string) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a cache file we wrote
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "cache_read", err.Error())
		return
	}
	cache.ServeBytes(w, r, data, mime)
}

// writeAssetErr maps catalog errors to the API envelope.
func (h *Handlers) writeAssetErr(w http.ResponseWriter, err error) {
	if errors.Is(err, catalog.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	httpx.WriteAPIError(w, err)
}
