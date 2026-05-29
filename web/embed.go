// Package web embeds the built frontend (web/dist) so the single binary can
// serve the SPA without any external files. The frontend teammate's Vite
// build writes into web/dist; this package only exposes it as an fs.FS.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the embedded production frontend rooted at the dist directory
// (so "index.html" is at the FS root). It panics only on a build that somehow
// embedded a malformed tree, which would be a programmer error.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("web: embedded dist tree is malformed: " + err.Error())
	}
	return sub
}
