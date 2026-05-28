// Package transform runs the heavy, worker-side media operations the durable
// job queue feeds it: video transcode, RAW develop and high-quality image
// conversion. It shells out to ffmpeg (with GPU acceleration where the box has
// it — NVENC/QSV/VAAPI — and a CPU fallback otherwise), dcraw/exiftool and
// darktable-cli, all detected at runtime and entirely optional. The serve host
// has no GPU and never runs these; only the relocatable enrich-worker does.
//
// Every external invocation goes through the small Runner seam so unit tests
// can drive a fake; the real tool runs live behind the `integration` build tag
// (see *_integration_test.go) so the default `go test` stays hermetic and
// CGO-free.
package transform

import (
	"context"
	"fmt"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// Request describes one transform to perform. SourcePath is an absolute,
// already-resolved on-disk path to the source the worker fetched (the worker
// stages the source it pulled from /jobs/{id}/source into a temp file).
// OutputDir is a writable scratch directory the runner may create the
// derivative in; nothing is ever written back into a media library.
type Request struct {
	Kind       string            // catalog JobKind* (transcode-video, develop-raw, convert-image)
	SourcePath string            // absolute path to staged source bytes
	SourceName string            // original base name (for extension/format hints)
	OutputDir  string            // scratch dir for the derivative
	Params     map[string]string // job params (e.g. format=webp, profile=hls)
}

// Result is what a Runner produces: the path to the derivative file it wrote,
// plus the catalog derivative kind and MIME type to record. OutputPath lives
// under Request.OutputDir; the worker reads it back and posts it to the job
// API, then cleans the scratch dir up.
type Result struct {
	OutputPath     string
	DerivativeKind string // catalog Derivative.Kind ("mp4","developed-jpg","webp",...)
	Mime           string
}

// Runner performs a single transform. The concrete implementation (Transformer)
// dispatches by Kind to ffmpeg/dcraw/etc.; unit tests inject a fake.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// param returns the param value for key, or def if absent/empty.
func param(p map[string]string, key, def string) string {
	if v, ok := p[key]; ok && v != "" {
		return v
	}
	return def
}

// validKinds is the set of transform kinds Transformer knows how to run. The
// enrich/face kinds are delegated by the worker to ai-people handlers, not run
// here.
var validKinds = map[string]bool{
	catalog.JobKindTranscodeVideo: true,
	catalog.JobKindDevelopRAW:     true,
	catalog.JobKindConvertImage:   true,
}

// IsTransformKind reports whether kind is a media transform this package runs
// (as opposed to an enrich/face job delegated elsewhere).
func IsTransformKind(kind string) bool { return validKinds[kind] }

// errUnsupportedKind is returned for a job kind transform does not handle.
func errUnsupportedKind(kind string) error {
	return fmt.Errorf("transform: unsupported kind %q", kind)
}
