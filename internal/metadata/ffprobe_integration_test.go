//go:build integration

package metadata_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/internaltest"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/metadata"
)

// TestFFprobeRealVideo runs the actual ffprobe binary over a real video copied
// from ~/Videos. It is integration-tagged (go test -tags integration) and skips
// if ffprobe is not installed or no sample video is available, so it never gates
// the default build (docs/specs/search-index.md "Tests").
func TestFFprobeRealVideo(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH; skipping integration test")
	}
	resolver, roots := newResolver(t)
	home := internaltest.SampleHome(t)
	names := internaltest.CopySampleMedia(t, filepath.Join(home, "Videos"), roots.Videos, 3)

	var video string
	for _, n := range names {
		if media.ClassifyExt(n) == catalog.FileKindVideo {
			video = n
			break
		}
	}
	if video == "" {
		t.Skip("no sample video under ~/Videos; skipping")
	}

	ex := metadata.NewExtractor(metadata.Options{Resolver: resolver})
	a := catalog.Asset{
		ID:          "v1",
		Alias:       "videos",
		BaseName:    video,
		Kind:        catalog.AssetKindVideo,
		DisplayPath: catalog.MediaPath("videos/" + video),
		Files:       []catalog.File{{MediaPath: catalog.MediaPath("videos/" + video), Kind: catalog.FileKindVideo}},
	}
	m, err := ex.Extract(context.Background(), a)
	if err != nil {
		t.Fatalf("extract video: %v", err)
	}
	if m.Source != metadata.SourceFFprobe {
		t.Fatalf("source = %q, want ffprobe", m.Source)
	}
	if m.DurationMs <= 0 && m.Codec == "" {
		t.Fatalf("expected duration or codec from ffprobe, got %+v", m)
	}
	t.Logf("ffprobe %s -> codec=%q %dx%d %dms captured=%v", video, m.Codec, m.Width, m.Height, m.DurationMs, m.CapturedAt)
}
