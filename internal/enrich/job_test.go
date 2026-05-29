package enrich_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/enrich"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// scriptedEnricher returns a fixed EnrichResult, recording the media path it saw.
type scriptedEnricher struct {
	res   catalog.EnrichResult
	mu    sync.Mutex
	calls []catalog.MediaPath
}

func (e *scriptedEnricher) Enrich(_ context.Context, mp catalog.MediaPath, _ io.Reader) (catalog.EnrichResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, mp)
	e.mu.Unlock()
	return e.res, nil
}

// compile-time check that scriptedEnricher satisfies the shared port.
var _ catalog.Enricher = (*scriptedEnricher)(nil)

// fakeLedger records enrichment runs for the result-handler cache test.
type fakeLedger struct {
	mu   sync.Mutex
	runs map[string]string // assetID|kind -> contentHash
}

func (l *fakeLedger) RecordRun(_ context.Context, assetID, kind, contentHash string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.runs == nil {
		l.runs = map[string]string{}
	}
	l.runs[assetID+"|"+kind] = contentHash
	return nil
}

// TestEnrichJobRoundTrip runs the worker handler over a staged file, round-trips
// the Data through JSON, then applies it via the result handler -- asserting the
// search sink received the caption/labels/embedding and the ledger recorded the run.
func TestEnrichJobRoundTrip(t *testing.T) {
	ctx := context.Background()
	enricher := &scriptedEnricher{res: catalog.EnrichResult{
		Labels:    []string{"cat", "indoor"},
		Caption:   "a cat on a sofa",
		Embedding: []float32{0.2, 0.4},
	}}

	src := filepath.Join(t.TempDir(), "asset.jpg")
	if err := os.WriteFile(src, []byte("img-bytes"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	wh := enrich.WorkerHandler(enricher)
	hres, err := wh(ctx, worker.JobView{ID: "j1", Kind: catalog.JobKindEnrichAI, AssetID: "a1", MediaPath: "pictures/cat.jpg"}, src)
	if err != nil {
		t.Fatalf("worker handler: %v", err)
	}

	// Carry Data over JSON like the job API would.
	wire, _ := json.Marshal(hres.Data)
	var jobData map[string]any
	if err := json.Unmarshal(wire, &jobData); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	sink := &fakeSink{}
	ledger := &fakeLedger{}
	rh := enrich.ResultHandler(sink, ledger)
	job := catalog.Job{ID: "j1", Kind: catalog.JobKindEnrichAI, AssetID: "a1", Params: map[string]string{"contentHash": "h123"}}
	if err := rh(ctx, job, catalog.JobResult{Data: jobData}); err != nil {
		t.Fatalf("result handler: %v", err)
	}

	if sink.caption != "a cat on a sofa" {
		t.Fatalf("caption = %q", sink.caption)
	}
	if sink.labels != "cat indoor" {
		t.Fatalf("labels = %q", sink.labels)
	}
	if !sink.embedded || len(sink.embedding) != 2 {
		t.Fatalf("embedding not applied: %+v", sink.embedding)
	}
	if ledger.runs["a1|"+catalog.JobKindEnrichAI] != "h123" {
		t.Fatalf("ledger run not recorded: %+v", ledger.runs)
	}
}
