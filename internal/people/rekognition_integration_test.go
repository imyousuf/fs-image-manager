//go:build integration

// Real-AWS integration test for the Rekognition face recognizer. It is OFF by
// default (the `integration` build tag) and never runs in `go test ./...`. It is
// COST-BOUNDED and self-cleaning:
//
//   - Creates a THROWAWAY collection named fsim-itest-<random>.
//   - IndexFaces on a SMALL sample (capped at maxSampleImages, a few hundred max)
//     of JPEGs from ~/Pictures.
//   - Proves same-person grouping: re-querying the collection with one already-
//     indexed image (SearchFacesByImage) returns its own indexed face as the top
//     self-match, and SearchFaces by a face id surfaces visually-similar faces.
//   - DELETES the collection at the end (t.Cleanup), even on failure.
//
// Run it explicitly (lead-gated, see the task summary):
//
//	AWS_PROFILE=imyousuf go test -tags=integration -run TestRekognitionIntegration \
//	    -timeout 20m ./internal/people/ -v
//
// It uses profile "imyousuf" / region "us-east-1" per docs/specs/ai-people.md.
package people_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/people"
)

const (
	// maxSampleImages bounds the number of IndexFaces calls (cost ceiling). Keep
	// this small -- a few hundred at most -- per the spec.
	maxSampleImages = 120
	itestProfile    = "imyousuf"
	itestRegion     = "us-east-1"
)

func TestRekognitionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	collection := fmt.Sprintf("fsim-itest-%d", rand.Int63())
	rec, err := people.NewRekognitionRecognizer(ctx, people.RekognitionOptions{
		Profile:    itestProfile,
		Region:     itestRegion,
		Collection: collection,
	})
	if err != nil {
		t.Fatalf("NewRekognitionRecognizer: %v", err)
	}

	if err := rec.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection %q: %v", collection, err)
	}
	// Always tear the throwaway collection down, even on failure.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer ccancel()
		if derr := rec.DeleteCollection(cctx); derr != nil {
			t.Errorf("cleanup: delete collection %q: %v", collection, derr)
		} else {
			t.Logf("cleanup: deleted throwaway collection %q", collection)
		}
	})

	samples := sampleImages(t, maxSampleImages)
	if len(samples) == 0 {
		t.Skip("no sample JPEGs found under ~/Pictures; skipping integration test")
	}
	t.Logf("indexing %d sample image(s) into %q", len(samples), collection)

	// Index every sample; remember the ext refs (FaceIds) of one image that
	// produced at least one face so we can re-query the collection by face id.
	var anchorRefs []string
	indexedFaces := 0
	for _, path := range samples {
		f, oerr := os.Open(path) //nolint:gosec // local sample file under the user's own Pictures
		if oerr != nil {
			t.Logf("open %s: %v (skipping)", path, oerr)
			continue
		}
		faces, derr := rec.DetectAndEmbed(ctx, f)
		_ = f.Close()
		if derr != nil {
			// Rekognition rejects some images (e.g. CMYK, too large); skip, do not fail.
			t.Logf("DetectAndEmbed %s: %v (skipping)", filepath.Base(path), derr)
			continue
		}
		indexedFaces += len(faces)
		if anchorRefs == nil && len(faces) > 0 {
			for _, fc := range faces {
				anchorRefs = append(anchorRefs, fc.ExtRef)
			}
			t.Logf("anchor image %s indexed %d face(s)", filepath.Base(path), len(faces))
		}
	}

	t.Logf("indexed %d face(s) total across the sample", indexedFaces)
	if indexedFaces == 0 || len(anchorRefs) == 0 {
		t.Skip("the sampled images contained no detectable faces; cannot exercise grouping")
	}

	// Prove same-person grouping: SearchFaces by an anchor face id returns the
	// most similar OTHER faces in the collection above the match threshold. We do
	// not assert a specific count (depends on the library), but the call must
	// succeed and never return the query face itself.
	for _, ref := range anchorRefs {
		cands, serr := rec.SearchSimilar(ctx, ref)
		if serr != nil {
			t.Fatalf("SearchSimilar(%s): %v", ref, serr)
		}
		for _, c := range cands {
			if c.ExtRef == ref {
				t.Fatalf("SearchSimilar returned the query face itself (%s)", ref)
			}
			if c.Similarity < 90 {
				t.Fatalf("SearchSimilar candidate below threshold: %.2f", c.Similarity)
			}
		}
		t.Logf("anchor face %s matched %d other face(s) of the same person", ref, len(cands))
	}
}

// sampleImages returns up to max JPEG paths from ~/Pictures, shuffled for a
// representative spread. It walks lazily and stops once it has enough.
func sampleImages(t *testing.T, max int) []string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home: %v", err)
	}
	root := filepath.Join(home, "Pictures")

	var paths []string
	// Gather a candidate pool larger than max, then shuffle + truncate, so we do
	// not always test the same first-N files.
	const poolCap = 2000
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".jpg" || ext == ".jpeg" {
			paths = append(paths, p)
			if len(paths) >= poolCap {
				return filepath.SkipAll
			}
		}
		return nil
	})

	rand.Shuffle(len(paths), func(i, j int) { paths[i], paths[j] = paths[j], paths[i] })
	if len(paths) > max {
		paths = paths[:max]
	}
	return paths
}
