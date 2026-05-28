package media

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/httpx"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// stream handles GET /api/assets/{id}/stream — the original bytes of the
// asset's primary media file, with HTTP Range support via http.ServeContent
// (so video players can seek). For an image asset the display source streams;
// for a video the video file streams.
func (h *Handlers) stream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	asset, err := h.repo.GetAsset(r.Context(), id)
	if err != nil {
		h.writeAssetErr(w, err)
		return
	}
	mp := streamSource(asset)
	if mp == "" {
		httpx.WriteError(w, http.StatusNotFound, "no_stream_source", "asset has no streamable file")
		return
	}
	h.serveOriginal(w, r, mp)
}

// serveOriginal opens mp through the traversal-proof resolver and serves it with
// Range support. ServeContent sets Content-Type, Content-Length, Accept-Ranges
// and handles If-Range/If-Modified-Since.
func (h *Handlers) serveOriginal(w http.ResponseWriter, r *http.Request, mp catalog.MediaPath) {
	f, err := h.resolver.Open(mp)
	if err != nil {
		h.writeOpenErr(w, err)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "stat_failed", err.Error())
		return
	}
	if info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "not_a_file", "path is a directory")
		return
	}
	name := path.Base(string(mp))
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// http.ServeContent needs an io.ReadSeeker; *os.File satisfies it. It does
	// the Range handling, including multi-range, transparently.
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// streamSource returns the media path to stream for an asset: the video file
// for a video, else the display source (JPG/PNG/other), else the first member.
func streamSource(a catalog.Asset) catalog.MediaPath {
	if a.DisplayPath != "" {
		return a.DisplayPath
	}
	// RAW-only or odd asset: fall back to the first non-sidecar member so the
	// original is still downloadable/streamable.
	for _, f := range a.Files {
		if f.Kind != catalog.FileKindSidecar {
			return f.MediaPath
		}
	}
	return ""
}

// downloadAsset handles GET /api/assets/{id}/download?variant=jpg|raw|both|video.
// A single requested variant streams the file directly; "both" (or multiple
// matches) returns a zip.
func (h *Handlers) downloadAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	asset, err := h.repo.GetAsset(r.Context(), id)
	if err != nil {
		h.writeAssetErr(w, err)
		return
	}
	variant := r.URL.Query().Get("variant")
	if variant == "" {
		variant = "both"
	}
	paths := selectVariant(asset, variant)
	if len(paths) == 0 {
		httpx.WriteError(w, http.StatusNotFound, "no_such_variant", "asset has no file for variant "+variant)
		return
	}
	if len(paths) == 1 {
		h.serveDownloadFile(w, r, paths[0])
		return
	}
	h.serveZip(w, r, paths, asset.BaseName+".zip")
}

// selectVariant maps a download variant to the asset's matching member paths.
func selectVariant(a catalog.Asset, variant string) []catalog.MediaPath {
	var out []catalog.MediaPath
	for _, f := range a.Files {
		switch variant {
		case "jpg":
			if f.Kind == catalog.FileKindJPG || f.Kind == catalog.FileKindPNG {
				out = append(out, f.MediaPath)
			}
		case "raw":
			if f.Kind == catalog.FileKindRAW {
				out = append(out, f.MediaPath)
			}
		case "video":
			if f.Kind == catalog.FileKindVideo {
				out = append(out, f.MediaPath)
			}
		case "both":
			if f.Kind != catalog.FileKindSidecar {
				out = append(out, f.MediaPath)
			}
		}
	}
	return out
}

// serveDownloadFile streams a single file as an attachment (no Range needed for
// a download, but ServeContent gives correct headers).
func (h *Handlers) serveDownloadFile(w http.ResponseWriter, r *http.Request, mp catalog.MediaPath) {
	f, err := h.resolver.Open(mp)
	if err != nil {
		h.writeOpenErr(w, err)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "stat_failed", err.Error())
		return
	}
	name := path.Base(string(mp))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// zipRequest is the POST /api/download body: a list of media paths to bundle.
type zipRequest struct {
	Paths []string `json:"paths"`
}

// downloadZip handles POST /api/download {paths:[MediaPath]} -> a zip of the
// validated paths. Every path is resolved through the resolver; any invalid /
// traversal path rejects the whole request.
func (h *Handlers) downloadZip(w http.ResponseWriter, r *http.Request) {
	var req zipRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(req.Paths) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "paths must be non-empty")
		return
	}
	paths := make([]catalog.MediaPath, 0, len(req.Paths))
	for _, raw := range req.Paths {
		mp, err := httpx.ParseMediaPath(h.resolver, raw)
		if err != nil {
			httpx.WriteAPIError(w, err)
			return
		}
		paths = append(paths, mp)
	}
	h.serveZip(w, r, paths, "download.zip")
}

// serveZip streams a zip archive of the given (already-validated) paths. Each
// entry uses its alias-prefixed media path as the in-zip name so duplicate base
// names across folders do not collide. A failure mid-stream cannot change the
// already-sent 200; such errors are logged via the response being truncated.
func (h *Handlers) serveZip(w http.ResponseWriter, _ *http.Request, paths []catalog.MediaPath, filename string) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	for _, mp := range paths {
		if err := h.addToZip(zw, mp); err != nil {
			// Header already committed; stop adding. The truncated zip signals
			// the failure to the client.
			return
		}
	}
}

// addToZip copies one resolved file into the zip under its media-path name.
func (h *Handlers) addToZip(zw *zip.Writer, mp catalog.MediaPath) error {
	f, err := h.resolver.Open(mp)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return errors.New("media: zip member not a file")
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = string(mp) // alias-prefixed, collision-free
	hdr.Method = zip.Deflate
	wtr, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(wtr, f)
	return err
}

// writeOpenErr maps resolver Open errors to the API envelope (traversal /
// missing alias -> 4xx; missing file -> 404; else 500).
func (h *Handlers) writeOpenErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "file not found")
	case errors.Is(err, mediapath.ErrUnknownAlias),
		errors.Is(err, mediapath.ErrEmptyPath),
		errors.Is(err, mediapath.ErrNoAlias),
		errors.Is(err, mediapath.ErrRelativePath),
		errors.Is(err, mediapath.ErrEscapesRoot):
		httpx.WriteAPIError(w, err)
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "open_failed", err.Error())
	}
}
