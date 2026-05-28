package people

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"math/rand"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rekognition"
	rektypes "github.com/aws/aws-sdk-go-v2/service/rekognition/types"
	"github.com/disintegration/imaging"
)

// This is an INTERNAL test (package people) so it can implement the unexported
// rekognitionAPI seam and call newRekognitionRecognizer to assert the bytes the
// recognizer hands to IndexFaces are within Rekognition's limits. No network.

// captureAPI is a fake rekognitionAPI that records the image bytes passed to
// IndexFaces, so a test can assert downscaling happened before the call.
type captureAPI struct {
	gotBytes []byte
}

func (c *captureAPI) IndexFaces(_ context.Context, in *rekognition.IndexFacesInput, _ ...func(*rekognition.Options)) (*rekognition.IndexFacesOutput, error) {
	if in.Image != nil {
		c.gotBytes = in.Image.Bytes
	}
	// Return one synthetic indexed face so DetectAndEmbed yields a result.
	return &rekognition.IndexFacesOutput{
		FaceRecords: []rektypes.FaceRecord{{
			Face: &rektypes.Face{
				FaceId:      aws.String("face-1"),
				Confidence:  aws.Float32(99),
				BoundingBox: &rektypes.BoundingBox{Left: aws.Float32(0.1), Top: aws.Float32(0.1), Width: aws.Float32(0.2), Height: aws.Float32(0.2)},
			},
		}},
	}, nil
}

func (c *captureAPI) CreateCollection(_ context.Context, _ *rekognition.CreateCollectionInput, _ ...func(*rekognition.Options)) (*rekognition.CreateCollectionOutput, error) {
	return &rekognition.CreateCollectionOutput{}, nil
}
func (c *captureAPI) DeleteCollection(_ context.Context, _ *rekognition.DeleteCollectionInput, _ ...func(*rekognition.Options)) (*rekognition.DeleteCollectionOutput, error) {
	return &rekognition.DeleteCollectionOutput{}, nil
}
func (c *captureAPI) SearchFaces(_ context.Context, _ *rekognition.SearchFacesInput, _ ...func(*rekognition.Options)) (*rekognition.SearchFacesOutput, error) {
	return &rekognition.SearchFacesOutput{}, nil
}
func (c *captureAPI) SearchFacesByImage(_ context.Context, _ *rekognition.SearchFacesByImageInput, _ ...func(*rekognition.Options)) (*rekognition.SearchFacesByImageOutput, error) {
	return &rekognition.SearchFacesByImageOutput{}, nil
}

// noiseJPEG builds a w x h JPEG of random pixels (high entropy so it does NOT
// compress to a trivially small size) — emulates a large DSLR capture. The pixel
// buffer is filled in one bulk rng.Read so the fixture stays cheap even under
// -race (a per-pixel loop here dominated the suite runtime).
func noiseJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // test fixture, determinism over crypto
	_, _ = rng.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255 // opaque alpha so JPEG encodes a clean RGB image
	}
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(100)); err != nil {
		t.Fatalf("encode noise jpeg: %v", err)
	}
	return buf.Bytes()
}

// solidJPEG builds a w x h JPEG of one colour (compresses tiny) for the
// dimension-only oversize case.
func solidJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := imaging.New(w, h, color.NRGBA{R: 120, G: 90, B: 60, A: 255})
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(90)); err != nil {
		t.Fatalf("encode solid jpeg: %v", err)
	}
	return buf.Bytes()
}

func dims(t *testing.T, raw []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return cfg.Width, cfg.Height
}

// TestPrepareDownscalesOversizeBytes: a >5 MiB JPEG is brought within the byte
// limit (and stays within the dimension cap).
func TestPrepareDownscalesOversizeBytes(t *testing.T) {
	// A 2200x2200 random-noise JPEG at q100 is ~9 MiB (well over the 5 MiB limit)
	// while staying within the dimension cap — isolates the byte-size path.
	raw := noiseJPEG(t, 2200, 2200)
	if len(raw) <= maxRekognitionBytes {
		t.Fatalf("test fixture not large enough: %d bytes (want > %d)", len(raw), maxRekognitionBytes)
	}

	out, err := prepareForRekognition(raw)
	if err != nil {
		t.Fatalf("prepareForRekognition: %v", err)
	}
	if len(out) > maxRekognitionBytes {
		t.Fatalf("output %d bytes exceeds limit %d", len(out), maxRekognitionBytes)
	}
	w, h := dims(t, out)
	if w > maxRekognitionDim || h > maxRekognitionDim {
		t.Fatalf("output %dx%d exceeds dim cap %d", w, h, maxRekognitionDim)
	}
}

// TestPrepareDownscalesOversizeDimsOnly: an image under 5 MiB but with a side
// > 4096 px is still downscaled (Rekognition also rejects >4096 px).
func TestPrepareDownscalesOversizeDimsOnly(t *testing.T) {
	raw := solidJPEG(t, 8000, 2000) // tiny bytes, but 8000 px wide
	if len(raw) > maxRekognitionBytes {
		t.Fatalf("fixture unexpectedly large: %d bytes", len(raw))
	}
	w0, _ := dims(t, raw)
	if w0 <= maxRekognitionDim {
		t.Fatalf("fixture not wide enough: %d px", w0)
	}

	out, err := prepareForRekognition(raw)
	if err != nil {
		t.Fatalf("prepareForRekognition: %v", err)
	}
	w, h := dims(t, out)
	if w > maxRekognitionDim || h > maxRekognitionDim {
		t.Fatalf("output %dx%d exceeds dim cap %d", w, h, maxRekognitionDim)
	}
}

// TestPrepareInSpecPassThrough: an already-in-spec image is returned byte-for-byte
// (no recompression -> best match accuracy).
func TestPrepareInSpecPassThrough(t *testing.T) {
	raw := noiseJPEG(t, 800, 600) // small dims; well under 5 MiB
	if len(raw) > maxRekognitionBytes {
		t.Fatalf("fixture unexpectedly large: %d bytes", len(raw))
	}
	out, err := prepareForRekognition(raw)
	if err != nil {
		t.Fatalf("prepareForRekognition: %v", err)
	}
	if !bytes.Equal(out, raw) {
		t.Fatalf("in-spec image should pass through unchanged (got %d bytes, want %d)", len(out), len(raw))
	}
}

// TestPrepareRejectsNonImage: bytes that are not a decodable image error out
// (Rekognition would reject them too; better to fail before the AWS call).
func TestPrepareRejectsNonImage(t *testing.T) {
	if _, err := prepareForRekognition([]byte("this is not an image")); err == nil {
		t.Fatalf("expected error for non-image bytes")
	}
}

// TestDetectAndEmbedDownscalesBeforeIndex: the END-TO-END assertion the task asks
// for — DetectAndEmbed downscales a large DSLR-sized image so the bytes IndexFaces
// actually receives are <= 5 MiB.
func TestDetectAndEmbedDownscalesBeforeIndex(t *testing.T) {
	// 4200x1400 noise exceeds BOTH limits at once: ~11 MiB (> 5 MiB) and 4200 px
	// wide (> 4096), so the recognizer must shrink dims AND re-encode.
	raw := noiseJPEG(t, 4200, 1400)
	if len(raw) <= maxRekognitionBytes {
		t.Fatalf("fixture not large enough: %d bytes", len(raw))
	}
	if w, _ := dims(t, raw); w <= maxRekognitionDim {
		t.Fatalf("fixture not wide enough: %d px", w)
	}
	api := &captureAPI{}
	rec := newRekognitionRecognizer(api, RekognitionOptions{Collection: "test-coll"})

	faces, err := rec.DetectAndEmbed(context.Background(), bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DetectAndEmbed: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("expected 1 face, got %d", len(faces))
	}
	if api.gotBytes == nil {
		t.Fatalf("IndexFaces never received image bytes")
	}
	if len(api.gotBytes) > maxRekognitionBytes {
		t.Fatalf("IndexFaces received %d bytes, exceeds Rekognition limit %d", len(api.gotBytes), maxRekognitionBytes)
	}
	w, h := dims(t, api.gotBytes)
	if w > maxRekognitionDim || h > maxRekognitionDim {
		t.Fatalf("IndexFaces received %dx%d, exceeds dim cap %d", w, h, maxRekognitionDim)
	}
}

// compile-time: captureAPI satisfies the unexported rekognitionAPI seam.
var _ rekognitionAPI = (*captureAPI)(nil)
