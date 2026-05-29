package worker

import (
	"context"
	"errors"
	"log/slog"

	"github.com/imyousuf/fs-image-manager/internal/transform"
)

// Config holds the resolved settings for a worker process (parsed from the
// enrich-worker flags / config by the CLI).
type Config struct {
	// Server is the serve base URL, e.g. "https://media-server:8080".
	Server string
	// Secret is the shared worker secret guarding the internal job API.
	Secret string
	// Options tune concurrency/lease/backoff; zero values get sane defaults.
	Options Options
	// Setup is an optional hook invoked on the freshly-built Worker, after its
	// transform Runner is installed but before it starts claiming, so the
	// assembly layer can register non-transform handlers (enrich-ai, face-index)
	// from [ai] config. It is the seam that keeps internal/worker free of
	// internal/enrich and internal/people: those packages import worker (for
	// worker.Handler / RegisterWorkerHandler), so worker must not import them —
	// the package-main assembly that imports all three builds the handlers and
	// hands them in here. nil means "transform kinds only" (the prior behaviour).
	Setup func(*Worker)
}

// Run builds and runs a worker until ctx is cancelled. It probes the box's
// transform capabilities (ffmpeg/GPU/dcraw/...) and uses the real Transformer.
// The default CLI path runs only the media transform kinds; the optional
// cfg.Setup hook registers ai-people's enrich/face handlers (see the
// package-main worker assembly and RegisterHandler) when [ai] config is present.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Server == "" {
		return errors.New("worker: --server is required")
	}
	if cfg.Secret == "" {
		return errors.New("worker: --secret is required")
	}

	caps := transform.Detect(ctx)
	logCapabilities(cfg.Options.Logger, caps)

	client := NewClient(cfg.Server, cfg.Secret, nil)
	runner := transform.New(caps)
	w := New(client, runner, cfg.Options)
	if cfg.Setup != nil {
		cfg.Setup(w)
	}
	return w.Run(ctx)
}

// logCapabilities reports what the box can do at startup so an operator can see
// whether GPU acceleration and the RAW develop tools were detected.
func logCapabilities(log *slog.Logger, caps transform.Capabilities) {
	if log == nil {
		log = slog.Default()
	}
	if !caps.HasFFmpeg() {
		log.Warn("worker: ffmpeg not found; transcode/convert/develop-raw fallback unavailable")
	}
	log.Info("worker capabilities",
		"ffmpeg", caps.FFmpeg != "",
		"ffprobe", caps.FFprobe != "",
		"video_accel", string(caps.VideoAccel),
		"h264", caps.H264Encoder(),
		"hevc", caps.HEVCEncoder(),
		"dcraw", caps.DCRaw != "",
		"exiftool", caps.ExifTool != "",
		"darktable", caps.DarktableCLI != "",
	)
}
