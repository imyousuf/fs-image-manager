package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// ErrFFmpegUnavailable is returned by VideoPoster when no ffmpeg binary is on
// PATH. Callers degrade gracefully (placeholder poster) rather than failing —
// ffmpeg is an optional runtime tool (docs/specs/_contracts.md §0).
var ErrFFmpegUnavailable = errors.New("media: ffmpeg not available")

// posterTimeout bounds a single poster grab so a pathological file cannot hang
// the host. A single-frame decode is cheap; this is a generous ceiling.
const posterTimeout = 30 * time.Second

var (
	ffmpegOnce sync.Once
	ffmpegPath string
)

// ffmpegBinary resolves (and caches) the ffmpeg path, "" if absent.
func ffmpegBinary() string {
	ffmpegOnce.Do(func() {
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			ffmpegPath = p
		}
	})
	return ffmpegPath
}

// FFmpegAvailable reports whether an ffmpeg binary was found on PATH. Used by
// callers/tests to decide whether to expect a real poster or a placeholder.
func FFmpegAvailable() bool { return ffmpegBinary() != "" }

// VideoPoster grabs a single frame from the video at absPath and returns it as
// a JPEG no larger than size on its longest edge. It shells out to ffmpeg; if
// ffmpeg is absent it returns ErrFFmpegUnavailable so the caller can fall back
// to a placeholder. absPath must come from the sanctioned resolver.
func VideoPoster(ctx context.Context, absPath string, size int) ([]byte, error) {
	bin := ffmpegBinary()
	if bin == "" {
		return nil, ErrFFmpegUnavailable
	}
	if size <= 0 {
		size = DefaultThumbSize
	}
	ctx, cancel := context.WithTimeout(ctx, posterTimeout)
	defer cancel()

	// Seek a little into the clip (~1s) to skip black lead-in frames, grab one
	// frame, scale to fit the box preserving aspect, and emit a JPEG to stdout.
	// scale uses -1 to keep aspect; force_original_aspect_ratio=decrease bounds
	// both edges by size.
	vf := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", size, size)
	args := []string{
		"-loglevel", "error",
		"-ss", "1",
		"-i", absPath,
		"-frames:v", "1",
		"-vf", vf,
		"-f", "image2",
		"-vcodec", "mjpeg",
		"-q:v", "3",
		"pipe:1",
	}
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // bin from LookPath, args fixed; absPath from resolver
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("media: ffmpeg poster %q: %w (%s)", absPath, err, errBuf.String())
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("media: ffmpeg produced no poster for %q", absPath)
	}
	return out.Bytes(), nil
}
