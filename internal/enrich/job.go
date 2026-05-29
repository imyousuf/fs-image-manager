package enrich

import (
	"context"
	"fmt"
	"os"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// enrich-ai is a two-sided job (see docs/TECH_SPEC.md section 9.1):
//
//   - On the WORKER (GPU box): WorkerHandler runs the Enricher over the staged
//     source file and ships the EnrichResult back as structured job Data (there
//     is no derivative file). The worker has the GPU and the Ollama endpoint; the
//     serve host has neither.
//   - On the SERVE host: ResultHandler decodes that Data and writes it to the
//     search index (FTS text + embedding). The jobs Registry invokes it when the
//     worker posts the result.
//
// The two halves are decoupled by the job queue; they never share a process in
// production. The shared wire shape is encode/decodeResult below.

// Result Data keys for the EnrichResult carried over the job API as a JSON
// object (catalog.JobResult.Data).
const (
	dataKeyCaption   = "caption"
	dataKeyOCR       = "ocr"
	dataKeyLabels    = "labels"
	dataKeyEmbedding = "embedding"
)

// WorkerHandler returns a worker.Handler for the enrich-ai kind: it opens the
// staged source file, runs the Enricher, and returns the result as structured
// Data (no derivative file). Register it with worker.RegisterHandler on the GPU
// box: w.RegisterHandler(catalog.JobKindEnrichAI, enrich.WorkerHandler(enricher)).
func WorkerHandler(enricher catalog.Enricher) worker.Handler {
	return func(ctx context.Context, job worker.JobView, sourcePath string) (worker.HandlerResult, error) {
		f, err := os.Open(sourcePath) //nolint:gosec // sourcePath is the worker's own staged temp file
		if err != nil {
			return worker.HandlerResult{}, fmt.Errorf("enrich: open source for %s: %w", job.AssetID, err)
		}
		defer func() { _ = f.Close() }()

		res, err := enricher.Enrich(ctx, catalog.MediaPath(job.MediaPath), f)
		if err != nil {
			return worker.HandlerResult{}, fmt.Errorf("enrich: enrich %s: %w", job.AssetID, err)
		}
		return worker.HandlerResult{Data: encodeResult(res)}, nil
	}
}

// ResultHandler returns a jobs.ResultHandler (jobs.Registry callback) for the
// enrich-ai kind: it decodes the EnrichResult from the job Data and applies it to
// the search index. Register it on the serve host:
//
//	reg.Register(catalog.JobKindEnrichAI, enrich.ResultHandler(searchIndex, repo))
//
// repo records the content-hash run so an unchanged asset is not re-enriched; it
// may be nil to skip the cache ledger.
func ResultHandler(sink SearchSink, ledger RunLedger) func(ctx context.Context, job catalog.Job, result catalog.JobResult) error {
	return func(ctx context.Context, job catalog.Job, result catalog.JobResult) error {
		res := decodeResult(result.Data)
		if err := Apply(ctx, sink, job.AssetID, res); err != nil {
			return err
		}
		if ledger != nil {
			if h := job.Params["contentHash"]; h != "" {
				if err := ledger.RecordRun(ctx, job.AssetID, catalog.JobKindEnrichAI, h); err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// RunLedger records that an enrichment kind ran over an asset's exact bytes so
// the enqueue path can skip unchanged media. *people.Repo satisfies it; nil
// disables the cache ledger.
type RunLedger interface {
	RecordRun(ctx context.Context, assetID, kind, contentHash string) error
}

// encodeResult serializes an EnrichResult to the JSON-able Data map carried by
// the job result. Labels and the embedding round-trip as []any (JSON arrays);
// decodeResult reverses it.
func encodeResult(res catalog.EnrichResult) map[string]any {
	data := map[string]any{
		dataKeyCaption: res.Caption,
		dataKeyOCR:     res.OCRText,
	}
	if len(res.Labels) > 0 {
		labels := make([]any, len(res.Labels))
		for i, l := range res.Labels {
			labels[i] = l
		}
		data[dataKeyLabels] = labels
	}
	if len(res.Embedding) > 0 {
		emb := make([]any, len(res.Embedding))
		for i, v := range res.Embedding {
			emb[i] = float64(v)
		}
		data[dataKeyEmbedding] = emb
	}
	return data
}

// decodeResult reverses encodeResult, tolerating a nil/partial map (a worker that
// produced no labels/embedding). It survives the JSON round-trip where numbers
// come back as float64 and arrays as []any.
func decodeResult(data map[string]any) catalog.EnrichResult {
	res := catalog.EnrichResult{
		Caption: asString(data[dataKeyCaption]),
		OCRText: asString(data[dataKeyOCR]),
	}
	if raw, ok := data[dataKeyLabels].([]any); ok {
		for _, v := range raw {
			if s := asString(v); s != "" {
				res.Labels = append(res.Labels, s)
			}
		}
	}
	if raw, ok := data[dataKeyEmbedding].([]any); ok {
		res.Embedding = make([]float32, 0, len(raw))
		for _, v := range raw {
			if f, ok := v.(float64); ok {
				res.Embedding = append(res.Embedding, float32(f))
			}
		}
	}
	return res
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
