package transform

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// Transformer is the production Runner. It dispatches a Request by kind to the
// right external tool, choosing GPU vs CPU paths from its Capabilities. The
// command lines are built by pure helpers (transcodeArgs/developArgs/
// convertArgs) so they can be unit-tested without invoking any tool; only Run
// actually executes, which the integration suite exercises against real files.
type Transformer struct {
	caps Capabilities
	// exec runs a command and returns combined output; overridable in tests.
	exec func(ctx context.Context, name string, args ...string) ([]byte, error)
}

var _ Runner = (*Transformer)(nil)

// New builds a Transformer with the given capabilities (use Detect to probe the
// box). For tests, set a fake runner via NewWithExec.
func New(caps Capabilities) *Transformer {
	return &Transformer{caps: caps, exec: runCommand}
}

// NewWithExec builds a Transformer with a custom command executor, letting unit
// tests assert the argv without running anything.
func NewWithExec(caps Capabilities, run func(ctx context.Context, name string, args ...string) ([]byte, error)) *Transformer {
	return &Transformer{caps: caps, exec: run}
}

// Run performs the transform described by req, writing the derivative under
// req.OutputDir and returning its path/kind/mime.
func (t *Transformer) Run(ctx context.Context, req Request) (Result, error) {
	switch req.Kind {
	case catalog.JobKindTranscodeVideo:
		return t.runTranscode(ctx, req)
	case catalog.JobKindDevelopRAW:
		return t.runDevelopRAW(ctx, req)
	case catalog.JobKindConvertImage:
		return t.runConvertImage(ctx, req)
	default:
		return Result{}, errUnsupportedKind(req.Kind)
	}
}

// --- transcode-video ---

// runTranscode produces a browser-friendly H.264/AAC MP4 (or an HLS rendition
// when profile=hls) from a source video, using the box's best video encoder.
func (t *Transformer) runTranscode(ctx context.Context, req Request) (Result, error) {
	if !t.caps.HasFFmpeg() {
		return Result{}, fmt.Errorf("transform: ffmpeg required for %s", req.Kind)
	}
	out := filepath.Join(req.OutputDir, "transcoded.mp4")
	derivKind := "mp4"
	mime := "video/mp4"
	if param(req.Params, "profile", "") == "hls" {
		out = filepath.Join(req.OutputDir, "index.m3u8")
		derivKind = "hls"
		mime = "application/vnd.apple.mpegurl"
	}
	args := transcodeArgs(t.caps, req.SourcePath, out, req.Params)
	if _, err := t.exec(ctx, t.caps.FFmpeg, args...); err != nil {
		return Result{}, fmt.Errorf("transform: transcode %s: %w", req.SourceName, err)
	}
	return Result{OutputPath: out, DerivativeKind: derivKind, Mime: mime}, nil
}

// transcodeArgs builds the ffmpeg argv for a transcode. It selects the H.264
// encoder from capabilities (NVENC/QSV/VAAPI/libx264), maps audio to AAC and
// targets a faststart MP4 (or HLS segments). The VAAPI path needs an extra
// hwupload filter and device init; QSV/NVENC accept frames directly.
func transcodeArgs(caps Capabilities, src, out string, params map[string]string) []string {
	enc := caps.H264Encoder()
	crf := param(params, "crf", "23")

	args := []string{"-hide_banner", "-y"}
	switch caps.VideoAccel {
	case HWVAAPI:
		args = append(args, "-vaapi_device", "/dev/dri/renderD128")
	case HWNVENC, HWQSV, HWNone:
		// no device init flags needed before -i
	}
	args = append(args, "-i", src)

	switch caps.VideoAccel {
	case HWVAAPI:
		args = append(args, "-vf", "format=nv12,hwupload", "-c:v", enc)
	case HWNVENC, HWQSV:
		args = append(args, "-c:v", enc, "-preset", "p4")
	default:
		args = append(args, "-c:v", enc, "-preset", "medium", "-crf", crf)
	}

	// Audio: re-encode to AAC stereo for broad browser support.
	args = append(args, "-c:a", "aac", "-b:a", "160k", "-movflags", "+faststart")

	if strings.HasSuffix(out, ".m3u8") {
		args = append(args,
			"-f", "hls",
			"-hls_time", param(params, "hls_time", "6"),
			"-hls_playlist_type", "vod",
		)
	}
	args = append(args, out)
	return args
}

// --- develop-raw ---

// runDevelopRAW turns a RAW-only capture into a viewable JPG. The fast path
// extracts the embedded full-size preview the camera already wrote (dcraw -e or
// exiftool), which is near-instant and visually identical to the in-camera JPG;
// the full-develop fallback (darktable-cli, else ffmpeg) demosaics the sensor
// data. The output is a managed derivative ("developed-jpg") and is NEVER
// written back into the media library.
func (t *Transformer) runDevelopRAW(ctx context.Context, req Request) (Result, error) {
	out := filepath.Join(req.OutputDir, "developed.jpg")
	prefer := param(req.Params, "develop", "preview") // "preview" | "full"

	if prefer != "full" {
		// Fast path: embedded preview via dcraw -e or exiftool.
		if t.caps.DCRaw != "" {
			args := dcrawPreviewArgs(req.SourcePath)
			if _, err := t.exec(ctx, t.caps.DCRaw, args...); err == nil {
				// dcraw -e writes "<base>.thumb.jpg" next to the source; the
				// worker stages sources in a private dir, so move it to out.
				if p, ok := dcrawThumbPath(req.SourcePath); ok {
					return Result{OutputPath: p, DerivativeKind: "developed-jpg", Mime: "image/jpeg"}, nil
				}
			}
		}
		if t.caps.ExifTool != "" {
			args := exiftoolPreviewArgs(req.SourcePath, out)
			if _, err := t.exec(ctx, t.caps.ExifTool, args...); err == nil {
				return Result{OutputPath: out, DerivativeKind: "developed-jpg", Mime: "image/jpeg"}, nil
			}
		}
	}

	// Full develop fallback.
	if t.caps.DarktableCLI != "" {
		args := darktableArgs(req.SourcePath, out)
		if _, err := t.exec(ctx, t.caps.DarktableCLI, args...); err == nil {
			return Result{OutputPath: out, DerivativeKind: "developed-jpg", Mime: "image/jpeg"}, nil
		}
	}
	if t.caps.HasFFmpeg() {
		args := ffmpegDevelopArgs(req.SourcePath, out)
		if _, err := t.exec(ctx, t.caps.FFmpeg, args...); err == nil {
			return Result{OutputPath: out, DerivativeKind: "developed-jpg", Mime: "image/jpeg"}, nil
		}
	}
	return Result{}, fmt.Errorf("transform: no RAW develop path available for %s (need dcraw/exiftool/darktable-cli/ffmpeg)", req.SourceName)
}

// dcrawPreviewArgs extracts the embedded preview JPEG: dcraw -e <src>.
func dcrawPreviewArgs(src string) []string { return []string{"-e", src} }

// dcrawThumbPath is where `dcraw -e` writes the extracted preview: the source
// path with its extension replaced by ".thumb.jpg".
func dcrawThumbPath(src string) (string, bool) {
	ext := filepath.Ext(src)
	if ext == "" {
		return "", false
	}
	return strings.TrimSuffix(src, ext) + ".thumb.jpg", true
}

// exiftoolPreviewArgs writes the largest embedded preview to out. -b emits the
// binary tag; PreviewImage (or JpgFromRaw on some makes) is the full preview.
func exiftoolPreviewArgs(src, out string) []string {
	return []string{"-b", "-PreviewImage", "-w", out, src}
}

// darktableArgs renders a full develop to a JPG with default processing.
func darktableArgs(src, out string) []string {
	return []string{src, out}
}

// ffmpegDevelopArgs is the last-resort develop: ffmpeg can decode some RAW via
// its image demuxer and emit a JPG. Quality is lower than a real developer but
// it keeps the worker functional on a minimal box.
func ffmpegDevelopArgs(src, out string) []string {
	return []string{"-hide_banner", "-y", "-i", src, "-frames:v", "1", out}
}

// --- convert-image ---

// runConvertImage produces a modern web image derivative (WebP by default, or
// AVIF when format=avif) from a source image via ffmpeg's encoders.
func (t *Transformer) runConvertImage(ctx context.Context, req Request) (Result, error) {
	if !t.caps.HasFFmpeg() {
		return Result{}, fmt.Errorf("transform: ffmpeg required for %s", req.Kind)
	}
	format := strings.ToLower(param(req.Params, "format", "webp"))
	var out, derivKind, mime string
	switch format {
	case "avif":
		out, derivKind, mime = filepath.Join(req.OutputDir, "converted.avif"), "avif", "image/avif"
	case "webp":
		out, derivKind, mime = filepath.Join(req.OutputDir, "converted.webp"), "webp", "image/webp"
	default:
		return Result{}, fmt.Errorf("transform: unsupported image format %q", format)
	}
	args := convertArgs(req.SourcePath, out, req.Params)
	if _, err := t.exec(ctx, t.caps.FFmpeg, args...); err != nil {
		return Result{}, fmt.Errorf("transform: convert %s: %w", req.SourceName, err)
	}
	return Result{OutputPath: out, DerivativeKind: derivKind, Mime: mime}, nil
}

// convertArgs builds the ffmpeg argv for image conversion, optionally scaling
// to a max width (param "width") while preserving aspect ratio.
func convertArgs(src, out string, params map[string]string) []string {
	args := []string{"-hide_banner", "-y", "-i", src}
	if w := param(params, "width", ""); w != "" {
		args = append(args, "-vf", fmt.Sprintf("scale=%s:-1", w))
	}
	if q := param(params, "quality", ""); q != "" {
		args = append(args, "-q:v", q)
	}
	args = append(args, out)
	return args
}

// runCommand executes name with args and returns combined stdout+stderr. On a
// non-zero exit the output is folded into the error so callers can log it.
func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
