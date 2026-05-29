// Package metadata extracts capture metadata from media files: EXIF for images
// (pure-Go via rwcarlsen/goexif) and, when ffprobe is present on the host,
// container/stream facts for video. It normalizes both into a single Meta and
// feeds the search index and the catalog's Asset.CapturedAt.
//
// It plugs into ingestion through internal/ingest's Indexer seam: after an asset
// is upserted the ingester calls IndexAsset, which extracts metadata, persists
// it, sets the asset's capture time and updates the full-text index. Extraction
// failures are non-fatal (logged by the ingester) so browsing never depends on
// it; a video with no ffprobe simply yields an empty Meta.
package metadata

import (
	"context"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// Source records which extractor produced a Meta. Empty means none ran (e.g. a
// video on a host with no ffprobe, or an image with unreadable EXIF).
type Source string

const (
	// SourceNone means no metadata was extracted.
	SourceNone Source = ""
	// SourceEXIF means the values came from image EXIF.
	SourceEXIF Source = "exif"
	// SourceFFprobe means the values came from ffprobe on a video.
	SourceFFprobe Source = "ffprobe"
)

// Meta is the normalized capture metadata for one asset. Zero values mean
// "unknown" for that field; CapturedAt is nil when no capture time was found.
// GPS is sparse on DSLR libraries, so the coordinates are pointers and usually
// nil (see docs/specs/search-index.md "Verified facts").
type Meta struct {
	CapturedAt  *time.Time
	CameraMake  string
	CameraModel string
	Lens        string
	Width       int
	Height      int
	Orientation int // EXIF orientation 1..8; 0 = unknown
	DurationMs  int64
	Codec       string
	GPSLat      *float64
	GPSLng      *float64
	Source      Source
}

// CameraLabel is the human-friendly camera string used for the camera facet and
// the FTS "camera" column: "<make> <model>" with the make trimmed when the model
// already begins with it (e.g. make "Canon", model "Canon EOS R5" -> "Canon EOS
// R5"). Returns "" when neither is known.
func (m Meta) CameraLabel() string {
	return joinCamera(m.CameraMake, m.CameraModel)
}

// Extractor produces Meta for the source file of an asset. Implementations are
// expected to be safe for concurrent use.
type Extractor interface {
	// Extract reads metadata for the asset, opening files through the resolver.
	// It returns a zero Meta (Source == SourceNone) and a nil error when the
	// asset carries no extractable metadata, reserving errors for genuine I/O or
	// decode failures the caller may want to log.
	Extract(ctx context.Context, a catalog.Asset) (Meta, error)
}

// extractor is the default Extractor: EXIF for images, ffprobe for video.
type extractor struct {
	resolver mediapath.Resolver
	ffprobe  *ffprobeExtractor
}

// Options configures the default extractor.
type Options struct {
	// Resolver opens library files traversal-proof. Required.
	Resolver mediapath.Resolver
	// FFprobePath overrides ffprobe discovery (mainly for tests). When empty the
	// extractor looks up "ffprobe" on PATH; if absent, video extraction is a
	// graceful no-op.
	FFprobePath string
}

// NewExtractor builds the default Extractor. Video extraction is enabled only if
// ffprobe is found (or FFprobePath is set); otherwise videos yield an empty Meta.
func NewExtractor(o Options) Extractor {
	return &extractor{
		resolver: o.Resolver,
		ffprobe:  newFFprobe(o.FFprobePath),
	}
}

// Extract dispatches on asset kind: images go through EXIF over the display (or
// a readable image member) file; videos go through ffprobe over the video member.
func (e *extractor) Extract(ctx context.Context, a catalog.Asset) (Meta, error) {
	switch a.Kind {
	case catalog.AssetKindVideo:
		f := videoMember(a)
		if f == "" {
			return Meta{}, nil
		}
		return e.ffprobe.extract(ctx, e.resolver, f)
	case catalog.AssetKindImage:
		f := exifMember(a)
		if f == "" {
			return Meta{}, nil
		}
		return extractEXIF(e.resolver, f)
	default:
		return Meta{}, nil
	}
}

// exifMember picks the file an image asset's EXIF should come from. We prefer a
// host-decodable still (JPG/PNG) -- the DisplayPath when set -- and otherwise
// fall back to the first non-sidecar member (e.g. a RAW, whose embedded EXIF
// goexif can often still read from the JPEG-app1 header). Sidecars are never
// read.
func exifMember(a catalog.Asset) catalog.MediaPath {
	if a.DisplayPath != "" {
		return a.DisplayPath
	}
	for _, f := range a.Files {
		if f.Kind == catalog.FileKindJPG || f.Kind == catalog.FileKindPNG {
			return f.MediaPath
		}
	}
	for _, f := range a.Files {
		if f.Kind == catalog.FileKindRAW {
			return f.MediaPath
		}
	}
	return ""
}

// videoMember returns the video file of a video asset, if any.
func videoMember(a catalog.Asset) catalog.MediaPath {
	if a.DisplayPath != "" && media.ClassifyExt(string(a.DisplayPath)) == catalog.FileKindVideo {
		return a.DisplayPath
	}
	for _, f := range a.Files {
		if f.Kind == catalog.FileKindVideo {
			return f.MediaPath
		}
	}
	return ""
}
