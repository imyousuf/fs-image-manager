package catalog_test

import (
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/media"
)

// in builds a FileInput for the given alias/dir/name; size/mtime are arbitrary
// but stable so grouping is deterministic.
func in(alias, dir, name string) catalog.FileInput {
	rel := name
	if dir != "" {
		rel = dir + "/" + name
	}
	return catalog.FileInput{
		Alias:     alias,
		Dir:       dir,
		Name:      name,
		MediaPath: catalog.MediaPath(alias + "/" + rel),
		Size:      1234,
		ModTime:   time.Unix(1600000000, 0),
	}
}

// TestGroupRawJpgXmpOneAsset is the headline regression (per media-pipeline
// spec): a CR3 + JPG + XMP sharing a basename collapse into ONE image asset
// whose DisplayPath is the JPG (never the RAW), with the XMP attached as a
// sidecar member and the asset NOT marked needs-develop.
func TestGroupRawJpgXmpOneAsset(t *testing.T) {
	inputs := []catalog.FileInput{
		in("pictures", "2021", "IMG_1234.CR3"),
		in("pictures", "2021", "IMG_1234.JPG"),
		in("pictures", "2021", "IMG_1234.xmp"),
	}
	assets := catalog.Group(inputs, media.ClassifyExt)

	if len(assets) != 1 {
		t.Fatalf("expected 1 asset, got %d: %+v", len(assets), assets)
	}
	a := assets[0]
	if a.Kind != catalog.AssetKindImage {
		t.Errorf("Kind = %q, want image", a.Kind)
	}
	if a.BaseName != "IMG_1234" {
		t.Errorf("BaseName = %q, want IMG_1234", a.BaseName)
	}
	if a.DisplayPath != "pictures/2021/IMG_1234.JPG" {
		t.Errorf("DisplayPath = %q, want the JPG", a.DisplayPath)
	}
	if catalog.NeedsDevelop(a) {
		t.Error("asset with a JPG must NOT need develop")
	}
	if len(a.Files) != 3 {
		t.Fatalf("expected 3 member files, got %d", len(a.Files))
	}
	var sawSidecar, sawRAW, sawJPG bool
	for _, f := range a.Files {
		switch f.Kind {
		case catalog.FileKindSidecar:
			sawSidecar = true
		case catalog.FileKindRAW:
			sawRAW = true
		case catalog.FileKindJPG:
			sawJPG = true
		}
	}
	if !sawSidecar || !sawRAW || !sawJPG {
		t.Errorf("members missing kinds: sidecar=%v raw=%v jpg=%v", sawSidecar, sawRAW, sawJPG)
	}
}

// TestGroupRawOnlyNeedsDevelop: a lone RAW (no JPG sibling) becomes an image
// asset with NO display source and is flagged needs-develop — we never pick the
// RAW as the host display source.
func TestGroupRawOnlyNeedsDevelop(t *testing.T) {
	assets := catalog.Group([]catalog.FileInput{in("pictures", "2021", "IMG_9.CR2")}, media.ClassifyExt)
	if len(assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(assets))
	}
	a := assets[0]
	if a.DisplayPath != "" {
		t.Errorf("RAW-only DisplayPath = %q, want empty", a.DisplayPath)
	}
	if !catalog.NeedsDevelop(a) {
		t.Error("RAW-only asset must need develop")
	}
}

// TestGroupThmSidecarAttachesToVideo: a .thm next to a video attaches to the
// video asset as a sidecar; it never forms its own asset.
func TestGroupThmSidecarAttachesToVideo(t *testing.T) {
	assets := catalog.Group([]catalog.FileInput{
		in("pictures", "clips", "MVI_0001.MOV"),
		in("pictures", "clips", "MVI_0001.THM"),
	}, media.ClassifyExt)
	if len(assets) != 1 {
		t.Fatalf("expected 1 video asset, got %d: %+v", len(assets), assets)
	}
	a := assets[0]
	if a.Kind != catalog.AssetKindVideo {
		t.Errorf("Kind = %q, want video", a.Kind)
	}
	if a.DisplayPath != "pictures/clips/MVI_0001.MOV" {
		t.Errorf("DisplayPath = %q, want the MOV", a.DisplayPath)
	}
	if len(a.Files) != 2 {
		t.Errorf("expected video + thm sidecar, got %d files", len(a.Files))
	}
}

// TestGroupSidecarOnlyDropped: a directory of only sidecars yields no assets.
func TestGroupSidecarOnlyDropped(t *testing.T) {
	assets := catalog.Group([]catalog.FileInput{
		in("pictures", "x", "orphan.xmp"),
		in("pictures", "x", "orphan.pp3"),
	}, media.ClassifyExt)
	// orphan.xmp and orphan.pp3 share basename "orphan" and are both sidecars;
	// with no media sibling the group is dropped.
	if len(assets) != 0 {
		t.Fatalf("sidecar-only group must not form an asset, got %d", len(assets))
	}
}

// TestGroupVideoAndImageSameBasenameSeparate: a JPG and a MOV sharing a
// basename are DISTINCT assets (different media kinds never merge).
func TestGroupVideoAndImageSameBasenameSeparate(t *testing.T) {
	assets := catalog.Group([]catalog.FileInput{
		in("pictures", "d", "SHOT.JPG"),
		in("pictures", "d", "SHOT.MOV"),
	}, media.ClassifyExt)
	if len(assets) != 2 {
		t.Fatalf("image + video at same basename must be 2 assets, got %d", len(assets))
	}
	kinds := map[string]bool{}
	for _, a := range assets {
		kinds[a.Kind] = true
		if a.BaseName != "SHOT" {
			t.Errorf("BaseName = %q, want SHOT", a.BaseName)
		}
	}
	if !kinds[catalog.AssetKindImage] || !kinds[catalog.AssetKindVideo] {
		t.Errorf("expected one image and one video asset, got kinds %v", kinds)
	}
}

// TestAssetIDStable: the asset id depends only on (alias,dir,base), so adding a
// JPG to an existing RAW keeps the id.
func TestAssetIDStable(t *testing.T) {
	rawOnly := catalog.Group([]catalog.FileInput{in("pictures", "2021", "IMG_1.CR3")}, media.ClassifyExt)
	withJpg := catalog.Group([]catalog.FileInput{
		in("pictures", "2021", "IMG_1.CR3"),
		in("pictures", "2021", "IMG_1.JPG"),
	}, media.ClassifyExt)
	if rawOnly[0].ID != withJpg[0].ID {
		t.Errorf("asset id changed when a JPG joined: %s vs %s", rawOnly[0].ID, withJpg[0].ID)
	}
	if !catalog.NeedsDevelop(rawOnly[0]) {
		t.Error("RAW-only should need develop")
	}
	if catalog.NeedsDevelop(withJpg[0]) {
		t.Error("RAW+JPG should not need develop")
	}
}
