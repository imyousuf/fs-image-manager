// Package media holds the host-side, CPU-cheap media operations: classifying a
// file by extension, content hashing, and generating thumbnails/previews/posters
// for natively-decodable media. It deliberately NEVER decodes RAW (that is heavy
// worker work, per docs/TECH_SPEC.md §6/§7) — RAW files are classified and hashed
// here but their pixels are only produced by the develop-raw job on the worker.
package media

import (
	"path"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// Extension sets, lower-cased, no leading dot. These drive both file-kind
// classification and the asset Kind (image vs video). Sourced from
// docs/TECH_SPEC.md §6 ("supported inputs") and the media-pipeline spec.
var (
	jpgExts = map[string]bool{
		"jpg": true, "jpeg": true,
	}
	pngExts = map[string]bool{
		"png": true,
	}
	// rawExts are RAW formats; we catalog and download them but never decode
	// their pixels on the host.
	rawExts = map[string]bool{
		"cr2": true, "cr3": true, "nef": true, "arw": true,
		"raf": true, "orf": true, "dng": true,
	}
	videoExts = map[string]bool{
		"mp4": true, "mov": true, "mkv": true, "webm": true, "avi": true,
	}
	// sidecarExts are metadata companions that attach to an asset but never
	// form a standalone asset (docs/specs/_contracts.md §3).
	sidecarExts = map[string]bool{
		"xmp": true, "pp3": true, "thm": true,
	}
	// otherImageExts are images we can list but not necessarily decode cheaply
	// on the host (heic needs CGO/libheif); they still count as image members.
	otherImageExts = map[string]bool{
		"heic": true,
	}
)

// ext returns the lower-cased extension of name without the leading dot.
func ext(name string) string {
	e := path.Ext(name)
	if e == "" {
		return ""
	}
	return strings.ToLower(e[1:])
}

// ClassifyExt maps a filename (or path) to its catalog.FileKind by extension.
// Unknown extensions are FileKindOther.
func ClassifyExt(name string) catalog.FileKind {
	switch e := ext(name); {
	case jpgExts[e]:
		return catalog.FileKindJPG
	case pngExts[e]:
		return catalog.FileKindPNG
	case rawExts[e]:
		return catalog.FileKindRAW
	case videoExts[e]:
		return catalog.FileKindVideo
	case sidecarExts[e]:
		return catalog.FileKindSidecar
	case otherImageExts[e]:
		// Treated as a generic image member; "other" keeps it out of the
		// JPG/PNG decode fast paths while still grouping into an image asset.
		return catalog.FileKindOther
	default:
		return catalog.FileKindOther
	}
}

// IsImageKind reports whether a kind contributes to an image asset (and could
// serve as a display source if no JPG/PNG is present). RAW counts as an image
// member but is never a host-decodable display source.
func IsImageKind(k catalog.FileKind) bool {
	switch k {
	case catalog.FileKindJPG, catalog.FileKindPNG, catalog.FileKindRAW:
		return true
	default:
		return false
	}
}

// IsHostDecodable reports whether the host can decode this kind to pixels with
// the pure-Go image stack (JPG/PNG). RAW and "other" (e.g. heic) are not.
func IsHostDecodable(k catalog.FileKind) bool {
	return k == catalog.FileKindJPG || k == catalog.FileKindPNG
}

// IsRAW reports whether the kind is a RAW capture (host never decodes it).
func IsRAW(k catalog.FileKind) bool { return k == catalog.FileKindRAW }

// IsSidecar reports whether the kind is a metadata sidecar.
func IsSidecar(k catalog.FileKind) bool { return k == catalog.FileKindSidecar }

// IsVideo reports whether the kind is a video.
func IsVideo(k catalog.FileKind) bool { return k == catalog.FileKindVideo }

// IsOtherImageExt reports whether name is a non-JPG/PNG image extension (e.g.
// heic) — an image member that still groups into an image asset but is not
// host-decodable. Sidecars and unknown files return false.
func IsOtherImageExt(name string) bool {
	return otherImageExts[ext(name)]
}
