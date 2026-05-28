package enrich

import (
	"context"
	"fmt"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// SearchSink is the slice of the search index the enrichment layer writes to. It
// is declared here (rather than imported as *search.Index) so the indexing logic
// is testable with a fake and enrich does not hard-depend on search's internals.
// *search.Index satisfies it: UpsertEnrichmentText fills the FTS labels/caption/
// ocr columns (preserving the persons column that internal/people owns), and
// UpsertEmbedding stores the semantic vector. Keeping the persons column out of
// this call is deliberate -- enrich-ai and face-index write disjoint columns of
// the same FTS row, so neither job may clobber the other's text.
type SearchSink interface {
	UpsertEnrichmentText(ctx context.Context, assetID, labels, caption, ocr string) error
	UpsertEmbedding(ctx context.Context, assetID string, vec []float32) error
}

// Apply writes an EnrichResult to the search index for an asset: the labels/
// caption/OCR become FTS-searchable text (the persons column is left untouched
// here -- internal/people owns it) and the embedding, if any, goes to the vector
// store. It is the single place enrichment output meets search, shared by the
// job result-handler and any synchronous (in-process) enrichment path.
//
// A zero result (NoopEnricher's output) still calls UpsertEnrichmentText with
// empty text, which is idempotent and harmless: it refreshes the asset's
// enrichment columns to empty without disturbing the search-index-owned
// name/camera/lens or the people-owned persons column.
func Apply(ctx context.Context, sink SearchSink, assetID string, res catalog.EnrichResult) error {
	if err := sink.UpsertEnrichmentText(ctx, assetID, joinLabels(res.Labels), res.Caption, res.OCRText); err != nil {
		return fmt.Errorf("enrich: index enrichment %s: %w", assetID, err)
	}
	if len(res.Embedding) > 0 {
		if err := sink.UpsertEmbedding(ctx, assetID, res.Embedding); err != nil {
			return fmt.Errorf("enrich: store embedding %s: %w", assetID, err)
		}
	}
	return nil
}

// joinLabels renders a label slice into a single space-joined string for the FTS
// labels column (FTS5 tokenizes on whitespace, so distinct labels stay distinct
// terms). Empty/blank labels are dropped.
func joinLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " ")
}
