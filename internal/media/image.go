package media

import (
	"bytes"
	"fmt"
	"image"
	"io"

	"github.com/disintegration/imaging"
)

// Default sizes for the two managed image derivatives. Thumbnails feed the grid;
// previews feed the lightbox/detail view. Both are bounded by the longest edge
// so aspect ratio is preserved.
const (
	// DefaultThumbSize is the thumbnail longest-edge in pixels.
	DefaultThumbSize = 320
	// DefaultPreviewSize is the display-size preview longest-edge in pixels.
	DefaultPreviewSize = 1600
	// jpegQuality is the encode quality for generated JPEG derivatives.
	jpegQuality = 85
)

// MimeJPEG is the content type of every image derivative this package emits.
const MimeJPEG = "image/jpeg"

// Thumbnail decodes the image in r and produces a JPEG thumbnail whose longest
// edge is at most size pixels (never upscaling). It returns the encoded bytes.
// Only host-decodable formats (JPG/PNG) should be passed; RAW must be developed
// on the worker first.
func Thumbnail(r io.Reader, size int) ([]byte, error) {
	return scaleJPEG(r, size)
}

// Preview is Thumbnail at the larger display size; separated for call-site
// clarity and so the default sizes can diverge.
func Preview(r io.Reader, size int) ([]byte, error) {
	return scaleJPEG(r, size)
}

// scaleJPEG decodes r, fits it within a size-by-size box (longest edge = size,
// aspect preserved, no upscaling) and JPEG-encodes the result.
func scaleJPEG(r io.Reader, size int) ([]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("media: invalid size %d", size)
	}
	img, err := imaging.Decode(r, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("media: decode image: %w", err)
	}
	fitted := fit(img, size)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, fitted, imaging.JPEG, imaging.JPEGQuality(jpegQuality)); err != nil {
		return nil, fmt.Errorf("media: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// fit scales img so its longest edge is at most size, preserving aspect ratio
// and never upscaling a smaller source.
func fit(img image.Image, size int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size {
		return img
	}
	// imaging.Fit bounds both dimensions by (size,size) using a high-quality
	// Lanczos filter and never upscales.
	return imaging.Fit(img, size, size, imaging.Lanczos)
}
