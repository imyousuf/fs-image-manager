package media

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/httpx"
)

// maxUploadBytes caps a single uploaded file. DSLR originals are large but not
// unbounded; this keeps a single request from exhausting disk.
const maxUploadBytes = 2 << 30 // 2 GiB

// uploadResult is the JSON returned per uploaded file.
type uploadResult struct {
	MediaPath string `json:"mediaPath"`
	Size      int64  `json:"size"`
}

// upload handles POST /api/upload?alias=<alias>&dir=<relpath> (multipart). It
// writes each "file" part into the library via the traversal-proof resolver,
// then triggers a reconcile so the new asset appears in listings immediately.
func (h *Handlers) upload(w http.ResponseWriter, r *http.Request) {
	alias := r.URL.Query().Get("alias")
	dir := r.URL.Query().Get("dir")
	if alias == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "missing alias")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MiB in memory, rest spills to temp
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "invalid multipart form: "+err.Error())
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "no file part named 'file'")
		return
	}

	results := make([]uploadResult, 0, len(r.MultipartForm.File["file"]))
	for _, fh := range r.MultipartForm.File["file"] {
		// filepath.Base strips any client-sent directory components from the
		// filename so the destination cannot be steered outside dir.
		name := filepath.Base(fh.Filename)
		if name == "" || name == "." || name == ".." {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "bad upload filename")
			return
		}
		rel := name
		if dir != "" {
			rel = dir + "/" + name
		}
		mp := catalog.MediaPath(alias + "/" + rel)

		abs, _, _, err := h.resolver.Resolve(mp)
		if err != nil {
			httpx.WriteAPIError(w, err)
			return
		}
		if err := writeUpload(abs, fh); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "upload_failed", err.Error())
			return
		}
		results = append(results, uploadResult{MediaPath: string(mp), Size: fh.Size})
	}

	// Trigger ingest of the target library so uploads are catalogued before the
	// response returns. A nil hook (no watcher) is a no-op — the periodic /
	// debounced reconcile catches the file eventually.
	if h.onUpload != nil {
		h.onUpload(r.Context(), alias)
	}

	httpx.WriteJSON(w, http.StatusCreated, results)
}

// writeUpload streams a single multipart part to abs, creating parent dirs as
// needed and writing via a temp file + rename so the watcher never observes a
// partial file under the final name.
func writeUpload(abs string, fh *multipart.FileHeader) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	src, err := fh.Open()
	if err != nil {
		return fmt.Errorf("open part: %w", err)
	}
	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(abs), ".upload-*")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	n, err := io.Copy(tmp, io.LimitReader(src, maxUploadBytes+1))
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if n > maxUploadBytes {
		_ = tmp.Close()
		return errors.New("upload exceeds maximum size")
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
