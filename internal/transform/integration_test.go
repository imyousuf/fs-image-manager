//go:build integration

// These tests exercise the REAL external tools (ffmpeg/dcraw/exiftool) against
// small sample media. They are excluded from the default `go test` (which stays
// hermetic and CGO-free) and run only with `-tags integration` on a box that
// actually has the tools and sample files. RAW samples come from ~/Pictures
// (Canon CR2/CR3); a tiny mp4 is synthesised by ffmpeg so the transcode test is
// self-contained.
package transform

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

func TestIntegrationTranscodeRealFFmpeg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	caps := probe(ctx)
	if !caps.HasFFmpeg() {
		t.Skip("ffmpeg not installed")
	}

	// Synthesise a 1-second test video so we do not depend on a sample file.
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	gen := exec.CommandContext(ctx, caps.FFmpeg, "-y", "-f", "lavfi",
		"-i", "testsrc=duration=1:size=320x240:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:v", "libx264", "-c:a", "aac", "-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("could not synthesise test video: %v: %s", err, out)
	}

	tr := New(caps)
	res, err := tr.Run(ctx, Request{
		Kind: catalog.JobKindTranscodeVideo, SourcePath: src, SourceName: "src.mp4", OutputDir: dir,
	})
	if err != nil {
		t.Fatalf("transcode: %v", err)
	}
	info, err := os.Stat(res.OutputPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("transcode produced no output: %v", err)
	}
}

func TestIntegrationConvertImageRealFFmpeg(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	caps := probe(ctx)
	if !caps.HasFFmpeg() {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	gen := exec.CommandContext(ctx, caps.FFmpeg, "-y", "-f", "lavfi",
		"-i", "color=c=red:s=64x64:d=1", "-frames:v", "1", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("could not synthesise test image: %v: %s", err, out)
	}

	tr := New(caps)
	res, err := tr.Run(ctx, Request{
		Kind: catalog.JobKindConvertImage, SourcePath: src, SourceName: "src.png", OutputDir: dir,
		Params: map[string]string{"format": "webp"},
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if info, err := os.Stat(res.OutputPath); err != nil || info.Size() == 0 {
		t.Fatalf("convert produced no output: %v", err)
	}
}

func TestIntegrationDevelopRealRAW(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	caps := probe(ctx)
	// A real RAW developer is required: bare ffmpeg cannot decode modern Canon
	// CR3, so the ffmpeg fallback alone is not sufficient for this test.
	if caps.DCRaw == "" && caps.ExifTool == "" && caps.DarktableCLI == "" {
		t.Skip("no RAW develop tool (dcraw/exiftool/darktable-cli) available")
	}

	raw := findRAWSample(t)
	if raw == "" {
		t.Skip("no CR2/CR3 sample found under ~/Pictures")
	}

	dir := t.TempDir()
	// Stage the RAW into the scratch dir (dcraw -e writes next to the source).
	staged := filepath.Join(dir, filepath.Base(raw))
	if err := copyFile(raw, staged); err != nil {
		t.Fatalf("stage raw: %v", err)
	}

	tr := New(caps)
	res, err := tr.Run(ctx, Request{
		Kind: catalog.JobKindDevelopRAW, SourcePath: staged, SourceName: filepath.Base(raw), OutputDir: dir,
	})
	if err != nil {
		t.Fatalf("develop: %v", err)
	}
	if info, err := os.Stat(res.OutputPath); err != nil || info.Size() == 0 {
		t.Fatalf("develop produced no output at %s: %v", res.OutputPath, err)
	}
	if res.DerivativeKind != "developed-jpg" {
		t.Fatalf("unexpected derivative kind %q", res.DerivativeKind)
	}
}

func findRAWSample(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	pics := filepath.Join(home, "Pictures")
	var found string
	_ = filepath.WalkDir(pics, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".cr2" || ext == ".cr3" {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src from a trusted walk of the user's library
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst) //nolint:gosec // dst is inside a t.TempDir
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
