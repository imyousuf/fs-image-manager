package catalog

import (
	"context"
	"io"
	"time"
)

// This file declares the cross-cutting "ports" — the interfaces that connect
// the domain packages — in one neutral location so implementors and consumers
// never form an import cycle (e.g. media-pipeline produces jobs via Queue while
// jobs-worker implements Queue; both depend only on catalog). Implementations
// live in their owning packages (cache, jobs, enrich, people); the signatures
// here are the agreed contract from docs/specs/_contracts.md §4.

// Cache is the derivative (thumbnail/poster/etc.) cache. Implemented by
// internal/cache (media-pipeline).
type Cache interface {
	// Get returns the cached file path for key and whether it exists.
	Get(key string) (path string, ok bool)
	// Put stores r under key with the given mime type and returns its path.
	Put(key string, r io.Reader, mime string) (path string, err error)
	// Key composes a content-addressed cache key from its parts.
	Key(assetID, kind, params, srcHash string) string
}

// Queue is the durable job queue. Implemented by internal/jobs (jobs-worker).
// Producers (media-pipeline, search-index, ai-people) hold a catalog.Queue and
// may treat a nil Queue as "no worker configured" (guard before enqueue).
type Queue interface {
	// Enqueue adds a job of kind for assetID/mp with params and returns it.
	Enqueue(ctx context.Context, kind, assetID string, mp MediaPath, params map[string]string) (Job, error)
	// Claim leases up to n jobs matching kinds for the given lease duration.
	Claim(ctx context.Context, kinds []string, lease time.Duration, n int) ([]Job, error)
	// Complete records a successful result for job id.
	Complete(ctx context.Context, id string, result JobResult) error
	// Fail records a failure (with reason) for job id.
	Fail(ctx context.Context, id, reason string) error
}

// Enricher performs generic AI enrichment (tags/caption/OCR/embedding) over a
// media stream. Implemented by internal/enrich (ai-people); NoopEnricher is the
// default.
type Enricher interface {
	Enrich(ctx context.Context, mp MediaPath, r io.Reader) (EnrichResult, error)
}

// FaceRecognizer detects/embeds faces and matches them to people. Implemented
// by internal/people (ai-people).
type FaceRecognizer interface {
	// DetectAndEmbed finds faces in the media stream and embeds each.
	DetectAndEmbed(ctx context.Context, r io.Reader) ([]Face, error)
	// Match resolves a detected face to a person id with a confidence score.
	Match(ctx context.Context, f Face) (personID string, confidence float64, err error)
}
