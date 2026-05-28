package enrich

import (
	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/jobs"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// This file is the turnkey wiring the serve and worker assemblies call, so the
// platform-owned main.go never has to know the shape of an enrich result. The
// two sides are independent: serve installs the result-handler that writes the
// search index; the worker installs the handler that runs the Enricher.

// RegisterResultHandler installs the enrich-ai result-handler on the serve-side
// jobs registry: when the worker posts an enrich-ai result, its Data is decoded
// and written to the search index (FTS text + embedding), and the content-hash
// run is recorded via ledger (nil ledger disables the cache record). Call at
// serve startup:
//
//	enrich.RegisterResultHandler(reg, searchIndex, peopleRepo)
func RegisterResultHandler(reg *jobs.Registry, sink SearchSink, ledger RunLedger) {
	reg.Register(catalog.JobKindEnrichAI, ResultHandler(sink, ledger))
}

// RegisterWorkerHandler installs the enrich-ai worker handler on the worker:
// each claimed enrich-ai job runs the Enricher over the staged source and ships
// the result back. Call on the GPU/worker box before w.Run, only when an
// Enricher other than Noop is configured:
//
//	enrich.RegisterWorkerHandler(w, enrich.NewOllamaEnricher(opts))
func RegisterWorkerHandler(w *worker.Worker, enricher catalog.Enricher) {
	w.RegisterHandler(catalog.JobKindEnrichAI, WorkerHandler(enricher))
}
