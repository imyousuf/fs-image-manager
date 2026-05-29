package enrich_test

import (
	"github.com/imyousuf/fs-image-manager/internal/enrich"
	"github.com/imyousuf/fs-image-manager/internal/search"
)

// Compile-time contract check: the real *search.Index must satisfy enrich's
// SearchSink seam, so the serve wiring (enrich.RegisterResultHandler(reg,
// searchIndex, ...)) type-checks. Kept in the test binary so production enrich
// does not import search. If search-index changes UpsertEnrichmentText/
// UpsertEmbedding, this fails here rather than only at the main.go call site.
var _ enrich.SearchSink = (*search.Index)(nil)
