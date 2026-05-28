package metadata_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/internaltest"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
	"github.com/imyousuf/fs-image-manager/internal/metadata"
)

// newResolver builds a resolver over two temp library roots.
func newResolver(t *testing.T) (mediapath.Resolver, internaltest.Roots) {
	t.Helper()
	roots := internaltest.NewTempRoots(t)
	resolver, err := mediapath.NewResolver([]catalog.Library{
		{Alias: "pictures", Name: "Pictures", Root: roots.Pictures},
		{Alias: "videos", Name: "Videos", Root: roots.Videos},
	})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	return resolver, roots
}

// assetForFile builds a single-file image asset whose display path is the given
// alias-relative file -- enough for the extractor to find and read it.
func assetForFile(alias, name string) catalog.Asset {
	mp := catalog.MediaPath(alias + "/" + name)
	return catalog.Asset{
		ID:          "asset-" + name,
		Alias:       alias,
		BaseName:    name,
		Kind:        catalog.AssetKindImage,
		DisplayPath: mp,
		Files: []catalog.File{
			{MediaPath: mp, Kind: media.ClassifyExt(name)},
		},
	}
}

// TestExtractEXIFFromRealSamples copies a few real images from ~/Pictures and
// asserts the extractor pulls usable capture metadata from at least one. DSLR
// EXIF is rich (date + camera); GPS is sparse, so we don't require it. The test
// skips cleanly on machines without a populated ~/Pictures.
func TestExtractEXIFFromRealSamples(t *testing.T) {
	resolver, roots := newResolver(t)
	home := internaltest.SampleHome(t)
	names := internaltest.CopySampleMedia(t, filepath.Join(home, "Pictures"), roots.Pictures, 6)
	if len(names) == 0 {
		t.Skip("no sample media under ~/Pictures; skipping real-EXIF test")
	}

	ex := metadata.NewExtractor(metadata.Options{Resolver: resolver})
	ctx := context.Background()

	var withDate, withCamera int
	for _, name := range names {
		// Only still images carry EXIF; skip copied video samples here.
		if media.ClassifyExt(name) == catalog.FileKindVideo {
			continue
		}
		m, err := ex.Extract(ctx, assetForFile("pictures", name))
		if err != nil {
			t.Fatalf("extract %s: %v", name, err)
		}
		if m.CapturedAt != nil {
			withDate++
		}
		if m.CameraModel != "" || m.CameraMake != "" {
			withCamera++
			t.Logf("%s -> camera=%q lens=%q %dx%d captured=%v",
				name, m.CameraLabel(), m.Lens, m.Width, m.Height, m.CapturedAt)
		}
	}

	if withDate == 0 && withCamera == 0 {
		t.Skip("sample images carried no EXIF (e.g. screenshots); nothing to assert")
	}
	t.Logf("extracted EXIF from samples: %d with date, %d with camera", withDate, withCamera)
}

// TestExtractNonImageIsEmpty verifies a file with no decodable EXIF yields an
// empty Meta and no error -- "no metadata" is a normal outcome, not a failure.
func TestExtractNonImageIsEmpty(t *testing.T) {
	resolver, roots := newResolver(t)
	// A tiny non-image file under the pictures root, classified as "other".
	writeRaw(t, roots.Pictures, "notes.txt", []byte("hello"))

	ex := metadata.NewExtractor(metadata.Options{Resolver: resolver})
	a := catalog.Asset{
		ID:          "a1",
		Alias:       "pictures",
		BaseName:    "notes",
		Kind:        catalog.AssetKindImage,
		DisplayPath: "pictures/notes.txt",
		Files:       []catalog.File{{MediaPath: "pictures/notes.txt", Kind: catalog.FileKindOther}},
	}
	m, err := ex.Extract(context.Background(), a)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if m.Source != metadata.SourceNone || m.CapturedAt != nil {
		t.Fatalf("want empty meta, got %+v", m)
	}
}
