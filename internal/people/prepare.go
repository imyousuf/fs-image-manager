package people

import (
	"bytes"
	"fmt"
	"image"
	// Register the JPEG/PNG decoders for image.DecodeConfig's header read. imaging
	// pulls these in too, but importing them here keeps the header decode correct
	// regardless of imaging's internals.
	_ "image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
)

// Rekognition rejects an image passed as raw bytes that exceeds either limit:
// the byte payload must be <= 5 MiB ("Member must have length less than or equal
// to 5242880") and each side <= 4096 px. A DSLR JPEG is routinely 10-23 MB and
// 6000+ px wide, so almost the whole library would fail IndexFaces unless we
// downscale first. prepareForRekognition brings any image within both limits
// while preserving as much face detail as Rekognition can use.
const (
	maxRekognitionBytes = 5 * 1024 * 1024 // 5 MiB hard limit on image.bytes
	maxRekognitionDim   = 4096            // max px per side
	// jpegQualitySteps is the descending JPEG quality ladder tried when a
	// dimension-bounded re-encode is still over the byte limit. 90 is visually
	// lossless for face recognition; we rarely go below it.
)

var jpegQualitySteps = []int{90, 85, 80, 75, 70, 60}

// prepareForRekognition returns image bytes guaranteed within Rekognition's
// limits (<= 5 MiB and <= 4096 px per side). If the input is already within both
// limits it is returned unchanged (no recompression — a small in-spec JPEG keeps
// its original quality for the best match accuracy). Otherwise the image is
// decoded, fit within 4096x4096 (aspect preserved), and re-encoded as JPEG,
// stepping quality down until it fits the byte limit; if even the lowest quality
// is still too large, the dimensions are halved and the ladder retried.
//
// It returns an error only if the bytes cannot be decoded as an image at all
// (Rekognition would reject those too). The decode also implicitly validates the
// payload is a real image before we spend an AWS call on it.
func prepareForRekognition(raw []byte) ([]byte, error) {
	// Fast path: already in spec by bytes AND dimensions -> pass through.
	if len(raw) <= maxRekognitionBytes {
		cfg, err := decodeConfig(raw)
		if err != nil {
			return nil, fmt.Errorf("people: decode image header: %w", err)
		}
		if cfg.Width <= maxRekognitionDim && cfg.Height <= maxRekognitionDim {
			return raw, nil
		}
	}

	img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("people: decode image: %w", err)
	}

	// Fit within the dimension cap (aspect preserved; never upscales).
	bound := maxRekognitionDim
	for {
		fitted := imaging.Fit(img, bound, bound, imaging.Lanczos)
		for _, q := range jpegQualitySteps {
			var buf bytes.Buffer
			if err := imaging.Encode(&buf, fitted, imaging.JPEG, imaging.JPEGQuality(q)); err != nil {
				return nil, fmt.Errorf("people: encode image: %w", err)
			}
			if buf.Len() <= maxRekognitionBytes {
				return buf.Bytes(), nil
			}
		}
		// Even the lowest quality at this size is too big: halve the bound and
		// retry. Guard against an infinite loop on a pathological image.
		bound /= 2
		if bound < 256 {
			// Last resort: emit the smallest-quality version at the floor size;
			// if it is somehow still over, surface an error rather than send a
			// payload Rekognition will reject.
			var buf bytes.Buffer
			fitted := imaging.Fit(img, 256, 256, imaging.Lanczos)
			if err := imaging.Encode(&buf, fitted, imaging.JPEG, imaging.JPEGQuality(60)); err != nil {
				return nil, fmt.Errorf("people: encode image: %w", err)
			}
			if buf.Len() <= maxRekognitionBytes {
				return buf.Bytes(), nil
			}
			return nil, fmt.Errorf("people: cannot fit image within %d bytes even at 256px", maxRekognitionBytes)
		}
	}
}

// decodeConfig reads just the image header to get dimensions, avoiding a full
// decode on the fast path where the bytes are already within the byte limit and
// we only need to confirm the pixel dimensions are in spec too.
func decodeConfig(raw []byte) (image.Config, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	return cfg, err
}
