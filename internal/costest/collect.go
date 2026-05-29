package costest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// This file gathers the library facts (face-index-candidate image count, total
// bytes) by WALKING the filesystem under one or more root paths. It needs no
// database and no network, so it works against a fresh library and is trivial to
// test against a temp dir. Faces are not stored on disk, so the face count is
// always derived as an estimate (images * avg-faces) by the caller.

// displayImageExts is the set of DISPLAY image extensions counted as
// face-index candidates — the still images Rekognition would index. It
// deliberately EXCLUDES:
//
//   - RAW formats (.cr2/.cr3/.nef/.arw/.dng/...): a RAW is almost always paired
//     with a JPG of the same shot; counting both would double-count a single
//     photo for per-image Rekognition pricing. We count the display JPG/PNG/HEIC
//     only, i.e. roughly one per shot.
//   - Video (.mp4/.mov/...): IndexFaces here runs over still images, not video.
//   - Sidecars/other (.xmp/.pp3/.thm/...): metadata companions, not images.
//
// Extensions are compared lower-cased, with the leading dot. Keep this list
// small and explicit (no heavy image-library dependency) and add formats here as
// needed.
var displayImageExts = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".heic": {},
}

// IsDisplayImage reports whether name has a display-image extension (the
// face-index candidate set). Exported so the CLI/help and tests can reason about
// the same classification the walk uses.
func IsDisplayImage(name string) bool {
	_, ok := displayImageExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

// FSSource is the production InputSource: it walks Roots on disk, counts
// display-image files as face-index candidates, and sums the bytes of EVERY
// regular file under the roots (the whole tree is what syncs to cloud storage).
// The face count is estimated from the image count (faces are not on disk).
type FSSource struct {
	// Roots are absolute directory paths (or single files) to walk. For the
	// default flow these are the configured [libraries] roots; for a positional
	// path arg they are the given paths.
	Roots []string
	// AvgFacesPerImage drives the estimated face count. <= 0 uses the package
	// default.
	AvgFacesPerImage float64
}

// Collect walks the roots and returns the measured image count + total bytes
// (both marked actual, since they are measured from disk) with an ESTIMATED
// face count (marked estimate). It satisfies InputSource.
func (s FSSource) Collect(ctx context.Context) (Inputs, error) {
	var in Inputs
	in.ImagesAreActual = true
	in.SizeIsActual = true

	for _, root := range s.Roots {
		if err := walkRoot(ctx, root, &in); err != nil {
			return Inputs{}, err
		}
	}

	// Faces are not stored on disk; always estimate from the image count.
	in.Faces = EstimateFacesFromImages(in.Images, s.AvgFacesPerImage)
	in.FacesAreActual = false
	return in, nil
}

// walkRoot walks a single root path (a directory tree, or a single file) and
// accumulates the image count + total bytes into in. Unreadable entries surface
// as an error so the estimate is never silently short.
func walkRoot(ctx context.Context, root string, in *Inputs) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("costest: walk %q: %w", path, err)
		}
		// Honour cancellation on large trees.
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			return nil
		}
		// Only regular files contribute bytes; skip symlinks/devices/sockets so a
		// dangling or special entry never derails the walk or inflates the size.
		info, ierr := d.Info()
		if ierr != nil {
			return fmt.Errorf("costest: stat %q: %w", path, ierr)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in.SizeBytes += info.Size()
		if IsDisplayImage(d.Name()) {
			in.Images++
		}
		return nil
	})
}

// ValidateRoots checks that each root exists and is readable, returning a clear
// error before the (potentially long) walk begins. It is a courtesy so a typo'd
// path fails fast with a useful message rather than mid-walk.
func ValidateRoots(roots []string) error {
	if len(roots) == 0 {
		return fmt.Errorf("costest: no paths to walk (give a path argument or configure [libraries])")
	}
	for _, r := range roots {
		if _, err := os.Stat(r); err != nil {
			return fmt.Errorf("costest: path %q: %w", r, err)
		}
	}
	return nil
}
