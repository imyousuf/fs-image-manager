package transform

import (
	"context"
	"strings"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

func TestParseEncoders(t *testing.T) {
	out := `Encoders:
 V..... = Video
 ------
 V....D h264_nvenc           NVIDIA NVENC H.264 encoder
 V....D libx264              libx264 H.264
 A....D aac                  AAC (Advanced Audio Coding)
 VFS..D hevc_qsv             HEVC (Intel Quick Sync)
`
	enc := parseEncoders(out)
	for _, want := range []string{"h264_nvenc", "libx264", "aac", "hevc_qsv"} {
		if !enc[want] {
			t.Errorf("expected encoder %q to be parsed; got %v", want, enc)
		}
	}
	// Header words must not be treated as encoders.
	if enc["Encoders:"] || enc["Video"] {
		t.Errorf("header lines leaked into encoder set: %v", enc)
	}
}

func TestChooseAccelPrefersNVENC(t *testing.T) {
	accel, h264, hevc := chooseAccel(map[string]bool{"h264_nvenc": true, "hevc_nvenc": true, "h264_qsv": true})
	if accel != HWNVENC || h264 != "h264_nvenc" || hevc != "hevc_nvenc" {
		t.Fatalf("nvenc not preferred: %v %s %s", accel, h264, hevc)
	}
}

func TestChooseAccelFallsBackToCPU(t *testing.T) {
	accel, h264, hevc := chooseAccel(map[string]bool{"libx264": true})
	if accel != HWNone || h264 != "libx264" || hevc != "libx265" {
		t.Fatalf("cpu fallback wrong: %v %s %s", accel, h264, hevc)
	}
}

func TestChooseAccelVAAPIWithCPUHEVC(t *testing.T) {
	// h264_vaapi present but no hevc_vaapi -> HEVC falls back to libx265.
	accel, h264, hevc := chooseAccel(map[string]bool{"h264_vaapi": true})
	if accel != HWVAAPI || h264 != "h264_vaapi" || hevc != "libx265" {
		t.Fatalf("vaapi selection wrong: %v %s %s", accel, h264, hevc)
	}
}

func TestTranscodeArgsCPU(t *testing.T) {
	caps := Capabilities{FFmpeg: "ffmpeg", VideoAccel: HWNone, h264Encoder: "libx264"}
	args := transcodeArgs(caps, "/in.mov", "/out.mp4", map[string]string{"crf": "20"})
	joined := strings.Join(args, " ")
	mustContain(t, joined, "-i /in.mov")
	mustContain(t, joined, "-c:v libx264")
	mustContain(t, joined, "-crf 20")
	mustContain(t, joined, "-c:a aac")
	mustContain(t, joined, "+faststart")
	mustContain(t, joined, "/out.mp4")
}

func TestTranscodeArgsNVENC(t *testing.T) {
	caps := Capabilities{FFmpeg: "ffmpeg", VideoAccel: HWNVENC, h264Encoder: "h264_nvenc"}
	args := transcodeArgs(caps, "/in.mov", "/out.mp4", nil)
	joined := strings.Join(args, " ")
	mustContain(t, joined, "-c:v h264_nvenc")
	if strings.Contains(joined, "-crf") {
		t.Errorf("nvenc path should not use -crf: %s", joined)
	}
}

func TestTranscodeArgsVAAPIHasDeviceAndUpload(t *testing.T) {
	caps := Capabilities{FFmpeg: "ffmpeg", VideoAccel: HWVAAPI, h264Encoder: "h264_vaapi"}
	args := transcodeArgs(caps, "/in.mov", "/out.mp4", nil)
	joined := strings.Join(args, " ")
	mustContain(t, joined, "-vaapi_device")
	mustContain(t, joined, "hwupload")
	mustContain(t, joined, "-c:v h264_vaapi")
}

func TestTranscodeArgsHLS(t *testing.T) {
	caps := Capabilities{FFmpeg: "ffmpeg"}
	args := transcodeArgs(caps, "/in.mov", "/out/index.m3u8", nil)
	joined := strings.Join(args, " ")
	mustContain(t, joined, "-f hls")
	mustContain(t, joined, "-hls_time 6")
}

func TestConvertArgsScaleAndQuality(t *testing.T) {
	args := convertArgs("/in.jpg", "/out.webp", map[string]string{"width": "1280", "quality": "80"})
	joined := strings.Join(args, " ")
	mustContain(t, joined, "scale=1280:-1")
	mustContain(t, joined, "-q:v 80")
	mustContain(t, joined, "/out.webp")
}

// TestRunTranscodeUsesFakeExec verifies Run dispatches and shells out with the
// expected program + a derivative result, without invoking real ffmpeg.
func TestRunTranscodeUsesFakeExec(t *testing.T) {
	var calledName string
	var calledArgs []string
	fake := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calledName, calledArgs = name, args
		return nil, nil
	}
	tr := NewWithExec(Capabilities{FFmpeg: "/usr/bin/ffmpeg", h264Encoder: "libx264"}, fake)
	res, err := tr.Run(context.Background(), Request{
		Kind:       catalog.JobKindTranscodeVideo,
		SourcePath: "/tmp/in.mov",
		SourceName: "in.mov",
		OutputDir:  "/tmp/out",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if calledName != "/usr/bin/ffmpeg" {
		t.Fatalf("expected ffmpeg invocation, got %q", calledName)
	}
	if res.DerivativeKind != "mp4" || res.Mime != "video/mp4" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.OutputPath != "/tmp/out/transcoded.mp4" {
		t.Fatalf("unexpected output path: %s", res.OutputPath)
	}
	if len(calledArgs) == 0 {
		t.Fatal("no args passed to ffmpeg")
	}
}

func TestRunConvertImageFormats(t *testing.T) {
	fake := func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	tr := NewWithExec(Capabilities{FFmpeg: "ffmpeg"}, fake)

	res, err := tr.Run(context.Background(), Request{
		Kind: catalog.JobKindConvertImage, SourcePath: "/in.jpg", OutputDir: "/out",
		Params: map[string]string{"format": "avif"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.DerivativeKind != "avif" || res.Mime != "image/avif" {
		t.Fatalf("avif result wrong: %+v", res)
	}
}

func TestRunUnsupportedKind(t *testing.T) {
	tr := NewWithExec(Capabilities{FFmpeg: "ffmpeg"}, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	if _, err := tr.Run(context.Background(), Request{Kind: "bogus"}); err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

func TestRunTranscodeRequiresFFmpeg(t *testing.T) {
	tr := NewWithExec(Capabilities{}, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	if _, err := tr.Run(context.Background(), Request{Kind: catalog.JobKindTranscodeVideo}); err == nil {
		t.Fatal("expected error when ffmpeg absent")
	}
}

func TestDevelopRAWExiftoolFastPath(t *testing.T) {
	var name string
	fake := func(_ context.Context, n string, _ ...string) ([]byte, error) {
		name = n
		return nil, nil
	}
	// No dcraw; exiftool available -> embedded-preview fast path via exiftool.
	tr := NewWithExec(Capabilities{ExifTool: "/usr/bin/exiftool"}, fake)
	res, err := tr.Run(context.Background(), Request{
		Kind: catalog.JobKindDevelopRAW, SourcePath: "/in.cr3", SourceName: "in.cr3", OutputDir: "/out",
	})
	if err != nil {
		t.Fatalf("develop: %v", err)
	}
	if name != "/usr/bin/exiftool" {
		t.Fatalf("expected exiftool fast path, got %q", name)
	}
	if res.DerivativeKind != "developed-jpg" {
		t.Fatalf("unexpected derivative kind %q", res.DerivativeKind)
	}
}

func TestDevelopRAWNoToolsFails(t *testing.T) {
	tr := NewWithExec(Capabilities{}, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	if _, err := tr.Run(context.Background(), Request{Kind: catalog.JobKindDevelopRAW, SourceName: "x.cr3"}); err == nil {
		t.Fatal("expected error when no develop tool present")
	}
}

func TestIsTransformKind(t *testing.T) {
	if !IsTransformKind(catalog.JobKindTranscodeVideo) {
		t.Error("transcode should be a transform kind")
	}
	if IsTransformKind(catalog.JobKindEnrichAI) {
		t.Error("enrich-ai is delegated, not a transform kind")
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected to find %q in: %s", needle, haystack)
	}
}
