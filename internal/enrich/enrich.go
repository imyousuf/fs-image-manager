// Package enrich is the generic, pluggable AI enrichment layer (see
// docs/TECH_SPEC.md section 9.1): it turns a media stream into searchable text
// (labels, a caption, OCR) plus an embedding vector for semantic search. It is
// deliberately separate from people recognition (internal/people): a generic VLM
// describes an image but cannot reliably re-identify a face, so faces have their
// own pipeline.
//
// Two implementations of catalog.Enricher ship here:
//
//   - NoopEnricher is the default. It returns an empty result, so a stock install
//     does nothing beyond the EXIF/ffprobe metadata search-index already extracts.
//     Enrichment is opt-in: it needs a configured Ollama endpoint (and, in
//     practice, a GPU box).
//   - OllamaEnricher talks to a private Ollama HTTP server (the [ai] ollama_url):
//     a vision model produces a caption + tags, an embedding model produces the
//     semantic vector. No data leaves the user's network.
//
// Results feed search via the Indexer seam (see indexer.go): caption/tags/OCR
// into the FTS columns (search.UpsertEnrichment) and the vector into the
// embedding store (search.UpsertEmbedding). Enrichment runs as an "enrich-ai"
// job on the worker; the job result-handler (see job.go) applies the result.
package enrich

import (
	"context"
	"io"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// Compile-time assertion that both enrichers satisfy the shared port.
var (
	_ catalog.Enricher = NoopEnricher{}
	_ catalog.Enricher = (*OllamaEnricher)(nil)
)

// NoopEnricher is the default Enricher: it performs no enrichment and returns an
// empty result. A stock install relies on EXIF/ffprobe metadata only; AI
// enrichment is opt-in via a configured Ollama endpoint.
type NoopEnricher struct{}

// Enrich returns an empty EnrichResult. It drains and discards the reader so a
// caller streaming a large source is not left with a half-read body, matching
// the behaviour of a real enricher that consumes the stream.
func (NoopEnricher) Enrich(_ context.Context, _ catalog.MediaPath, r io.Reader) (catalog.EnrichResult, error) {
	if r != nil {
		_, _ = io.Copy(io.Discard, r)
	}
	return catalog.EnrichResult{}, nil
}
