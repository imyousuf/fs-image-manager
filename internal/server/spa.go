package server

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the embedded single-page app from fsys. Real files are
// served by http.FileServerFS; any path that does not correspond to an
// embedded file falls back to index.html so client-side routing works on deep
// links and reloads. Requests under /api or /internal never reach here (the
// root mux routes those first).
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if upath == "" {
			upath = "index.html"
		}
		if exists(fsys, upath) {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback: rewrite to index.html and let the file server emit it
		// with correct content type and caching headers.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

// exists reports whether name is a regular file present in fsys.
func exists(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false
		}
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return !info.IsDir()
}
