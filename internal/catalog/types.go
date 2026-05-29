// Package catalog holds the shared domain types used across the whole
// application: libraries, the alias-prefixed media path, files, assets,
// derivatives, and the people-recognition entities.
//
// The type definitions here are the integration contract agreed in
// docs/specs/_contracts.md §2 and §3. The catalog *repository* behaviour
// (persisting/loading assets) is owned by the media-pipeline teammate; this
// file is the foundation skeleton so every package can compile against a
// single, stable set of types. Treat the struct shapes as a shared contract:
// changes go through backend-platform + the lead.
package catalog

import "time"

// MediaPath is an alias-prefixed, library-relative path of the form
// "<alias>/<relpath>" — e.g. "pictures/2021/IMG_1234.CR3". The first segment
// is a library alias; the remainder is the path *inside* that library root.
//
// Relative paths are not supported: no leading "/", no "." or ".." segments
// and no alias-less paths. The only sanctioned way to turn a MediaPath into an
// on-disk path is internal/mediapath.Resolver, which is traversal-proof.
type MediaPath string

// Library is a configured media root: an alias, a human-friendly name and the
// absolute on-disk directory the alias maps to.
type Library struct {
	Alias string
	Name  string
	Root  string // absolute directory
}

// FileKind classifies a single file on disk.
type FileKind string

// Recognised file kinds. "other" covers anything not otherwise classified;
// "sidecar" covers metadata companions (.xmp/.pp3/.thm) that attach to an
// asset but never form their own asset.
const (
	FileKindJPG     FileKind = "jpg"
	FileKindPNG     FileKind = "png"
	FileKindRAW     FileKind = "raw"
	FileKindVideo   FileKind = "video"
	FileKindSidecar FileKind = "sidecar"
	FileKindOther   FileKind = "other"
)

// File is one file on disk that belongs to an asset.
type File struct {
	MediaPath MediaPath // alias/relpath
	Kind      FileKind
	Size      int64
	ModTime   time.Time
	Hash      string // content hash (xxhash/sha256 of head+size; see media spec)
}

// Asset is one logical photo or video. It may bundle several files together
// (for example a RAW capture, its JPG sibling and an XMP sidecar).
type Asset struct {
	ID          string // stable: hash of "<alias>/<dir>/<basename-without-ext>"
	Alias       string
	Dir         string // relpath of the containing directory
	BaseName    string // filename without extension
	Kind        string // "image" | "video"
	Files       []File // members (e.g. .CR3 + .JPG + .XMP)
	DisplayPath MediaPath
	CapturedAt  *time.Time // from metadata (search-index fills)
}

// Derivative is a cached transform output (thumbnail, poster, developed JPG,
// transcode, etc.), keyed by source content hash + kind + params.
type Derivative struct {
	AssetID string
	Kind    string // "thumb","poster","preview","webp","mp4","developed-jpg"
	Params  string // e.g. "w=320"
	Path    string // absolute path in the cache dir
	Mime    string
}

// Person is a named cluster of faces.
type Person struct {
	ID          string
	Name        string
	CoverFaceID string
}

// Face is a single detected face within an asset.
type Face struct {
	ID         string
	AssetID    string
	BBox       [4]float64 // x,y,w,h normalized
	PersonID   string     // "" = unknown
	Confidence float64
	ExtRef     string // Rekognition FaceId or local vector ref
}

// JobKind enumerates the heavy/worker job kinds. Producers enqueue these via a
// Queue; the worker claims and runs them. (See _contracts.md §4.)
const (
	JobKindTranscodeVideo = "transcode-video"
	JobKindDevelopRAW     = "develop-raw"
	JobKindConvertImage   = "convert-image"
	JobKindEnrichAI       = "enrich-ai"
	JobKindFaceIndex      = "face-index"
)

// Job is a unit of heavy work in the durable queue (owned conceptually by
// jobs-worker; the struct lives here so producers like media-pipeline and
// search-index can enqueue without importing the jobs package — avoiding a
// media↔jobs import cycle).
type Job struct {
	ID        string
	Kind      string
	AssetID   string
	MediaPath MediaPath
	Params    map[string]string
	Status    string
}

// JobResult is what a worker reports back when it completes a job: an optional
// derivative payload (Bytes) and/or structured Data (e.g. extracted metadata).
type JobResult struct {
	DerivativeKind string
	Mime           string
	Bytes          []byte
	Data           map[string]any
}

// EnrichResult is the output of generic AI enrichment (owned conceptually by
// ai-people). It carries tags/caption/OCR plus an embedding vector for
// semantic search.
type EnrichResult struct {
	Labels    []string
	Caption   string
	OCRText   string
	Embedding []float32
}
