package transform

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HWAccel identifies the hardware video-encode path a box offers. Detection
// prefers NVENC > QSV > VAAPI when several are present, falling back to CPU
// (libx264) when none are usable.
type HWAccel string

// Supported hardware-encode families plus the CPU fallback sentinel.
const (
	HWNone  HWAccel = ""      // CPU (software) encoding
	HWNVENC HWAccel = "nvenc" // NVIDIA NVENC
	HWQSV   HWAccel = "qsv"   // Intel Quick Sync
	HWVAAPI HWAccel = "vaapi" // VA-API (Intel/AMD on Linux)
)

// Capabilities records which external tools and GPU encoders the current box
// exposes. It is probed once (see Detect) and is safe to share read-only.
type Capabilities struct {
	FFmpeg       string // path to ffmpeg, "" if absent
	FFprobe      string // path to ffprobe, "" if absent
	DCRaw        string // path to dcraw, "" if absent
	ExifTool     string // path to exiftool, "" if absent
	DarktableCLI string // path to darktable-cli, "" if absent

	// VideoAccel is the best available hardware video encoder, HWNone for CPU.
	VideoAccel HWAccel
	// h264Encoder/hevcEncoder are the resolved ffmpeg encoder names for the
	// chosen accel (e.g. "h264_nvenc" or "libx264").
	h264Encoder string
	hevcEncoder string
}

// HasFFmpeg reports whether ffmpeg is available (required for transcode and the
// ffmpeg RAW/develop and image-convert fallbacks).
func (c Capabilities) HasFFmpeg() bool { return c.FFmpeg != "" }

// H264Encoder returns the ffmpeg encoder name to use for H.264 output.
func (c Capabilities) H264Encoder() string {
	if c.h264Encoder != "" {
		return c.h264Encoder
	}
	return "libx264"
}

// HEVCEncoder returns the ffmpeg encoder name to use for HEVC/H.265 output
// (the hardware encoder when available, else the libx265 CPU encoder).
func (c Capabilities) HEVCEncoder() string {
	if c.hevcEncoder != "" {
		return c.hevcEncoder
	}
	return "libx265"
}

// probeTimeout bounds each detection subprocess so a hung tool cannot stall
// worker startup.
const probeTimeout = 10 * time.Second

var (
	detectOnce sync.Once
	detected   Capabilities
)

// Detect probes the box for tools and GPU encoders. The result is cached after
// the first call (capabilities do not change during a worker's lifetime).
func Detect(ctx context.Context) Capabilities {
	detectOnce.Do(func() { detected = probe(ctx) })
	return detected
}

// probe performs the actual detection (separated from Detect so tests can call
// it directly without the once-cache).
func probe(ctx context.Context) Capabilities {
	c := Capabilities{
		FFmpeg:       lookPath("ffmpeg"),
		FFprobe:      lookPath("ffprobe"),
		DCRaw:        lookPath("dcraw"),
		ExifTool:     lookPath("exiftool"),
		DarktableCLI: lookPath("darktable-cli"),
	}
	if c.FFmpeg != "" {
		encoders := ffmpegEncoders(ctx, c.FFmpeg)
		c.VideoAccel, c.h264Encoder, c.hevcEncoder = chooseAccel(encoders)
	}
	return c
}

func lookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// ffmpegEncoders returns the set of encoder names ffmpeg reports. An error
// (ffmpeg missing/old) yields an empty set, which steers chooseAccel to CPU.
func ffmpegEncoders(ctx context.Context, ffmpeg string) map[string]bool {
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, ffmpeg, "-hide_banner", "-encoders")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // parse whatever was produced even on non-zero exit
	return parseEncoders(out.String())
}

// parseEncoders extracts encoder names from `ffmpeg -encoders` output. Each
// encoder line looks like " V....D h264_nvenc  NVIDIA NVENC ..."; the name is
// the second whitespace-separated token.
func parseEncoders(s string) map[string]bool {
	set := make(map[string]bool)
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// The flags token is letters/dots only (e.g. "V....D"); skip header and
		// separator lines that do not match that shape.
		flags := fields[0]
		if !isFlagsToken(flags) {
			continue
		}
		set[fields[1]] = true
	}
	return set
}

func isFlagsToken(tok string) bool {
	if len(tok) < 2 {
		return false
	}
	for _, r := range tok {
		if r != '.' && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// chooseAccel picks the best hardware encoder family present, returning the
// family and the concrete H.264/HEVC encoder names to use. CPU (libx264/libx265)
// is the fallback when no hardware encoder is available.
func chooseAccel(enc map[string]bool) (HWAccel, string, string) {
	switch {
	case enc["h264_nvenc"]:
		return HWNVENC, "h264_nvenc", pick(enc, "hevc_nvenc", "libx265")
	case enc["h264_qsv"]:
		return HWQSV, "h264_qsv", pick(enc, "hevc_qsv", "libx265")
	case enc["h264_vaapi"]:
		return HWVAAPI, "h264_vaapi", pick(enc, "hevc_vaapi", "libx265")
	default:
		return HWNone, "libx264", "libx265"
	}
}

// pick returns name if present in enc, else def.
func pick(enc map[string]bool, name, def string) string {
	if enc[name] {
		return name
	}
	return def
}
