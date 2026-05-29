package media_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"

	"github.com/imyousuf/fs-image-manager/internal/internaltest"
	"github.com/imyousuf/fs-image-manager/internal/media"
)

// makeJPEG writes a solid-colour w x h JPEG to a temp file and returns its path.
func makeJPEG(t *testing.T, w, h int) string {
	t.Helper()
	img := imaging.New(w, h, color.NRGBA{R: 10, G: 120, B: 200, A: 255})
	path := filepath.Join(t.TempDir(), "src.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create src: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode src: %v", err)
	}
	return path
}

func decodeDims(t *testing.T, b []byte) image.Rectangle {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode derivative: %v", err)
	}
	return img.Bounds()
}

// TestThumbnailFitsBox: a 1000x500 source thumbnailed to 320 fits within a
// 320x320 box, preserves aspect (longest edge = 320), and is a valid JPEG.
func TestThumbnailFitsBox(t *testing.T) {
	src := makeJPEG(t, 1000, 500)
	f, err := os.Open(src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	out, err := media.Thumbnail(f, 320)
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	b := decodeDims(t, out)
	if b.Dx() != 320 || b.Dy() != 160 {
		t.Errorf("thumb dims = %dx%d, want 320x160", b.Dx(), b.Dy())
	}
}

// TestThumbnailNoUpscale: a source smaller than the box is not upscaled.
func TestThumbnailNoUpscale(t *testing.T) {
	src := makeJPEG(t, 100, 80)
	f, _ := os.Open(src)
	defer func() { _ = f.Close() }()
	out, err := media.Thumbnail(f, 320)
	if err != nil {
		t.Fatalf("Thumbnail: %v", err)
	}
	b := decodeDims(t, out)
	if b.Dx() != 100 || b.Dy() != 80 {
		t.Errorf("dims = %dx%d, want unchanged 100x80", b.Dx(), b.Dy())
	}
}

// TestThumbnailRealJPEG decodes a real JPG sampled from ~/Pictures (skipped if
// none present) and asserts the thumbnail is a valid, in-bounds JPEG. This is
// the "golden" path against real DSLR media.
func TestThumbnailRealJPEG(t *testing.T) {
	home := internaltest.SampleHome(t)
	dst := t.TempDir()
	names := internaltest.CopySampleMedia(t, filepath.Join(home, "Pictures"), dst, 3)
	if len(names) == 0 {
		t.Skip("no sample media in ~/Pictures")
	}
	var jpgPath string
	for _, n := range names {
		ext := filepath.Ext(n)
		if ext == ".jpg" || ext == ".JPG" || ext == ".jpeg" || ext == ".JPEG" {
			jpgPath = filepath.Join(dst, n)
			break
		}
	}
	if jpgPath == "" {
		t.Skip("no JPG among sampled media")
	}
	f, err := os.Open(jpgPath)
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer func() { _ = f.Close() }()
	out, err := media.Thumbnail(f, media.DefaultThumbSize)
	if err != nil {
		t.Fatalf("Thumbnail(real): %v", err)
	}
	b := decodeDims(t, out)
	if b.Dx() > media.DefaultThumbSize || b.Dy() > media.DefaultThumbSize {
		t.Errorf("real thumb %dx%d exceeds box %d", b.Dx(), b.Dy(), media.DefaultThumbSize)
	}
	if b.Dx() == 0 || b.Dy() == 0 {
		t.Error("real thumb has a zero dimension")
	}
}

// TestPlaceholderIsValidJPEG: placeholders decode to the requested square.
func TestPlaceholderIsValidJPEG(t *testing.T) {
	for _, kind := range []string{media.PlaceholderVideo, media.PlaceholderRAW} {
		b := media.Placeholder(kind, 200)
		d := decodeDims(t, b)
		if d.Dx() != 200 || d.Dy() != 200 {
			t.Errorf("%s placeholder = %dx%d, want 200x200", kind, d.Dx(), d.Dy())
		}
	}
}
