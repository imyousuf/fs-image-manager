package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// fakeAISettings is a stand-in for *config.Config's [ai] accessors, so the
// worker AI wiring can be exercised without loading a config file.
type fakeAISettings struct {
	ollamaURL  string
	profile    string
	region     string
	collection string
}

func (f fakeAISettings) OllamaURL() string             { return f.ollamaURL }
func (f fakeAISettings) RekognitionProfile() string    { return f.profile }
func (f fakeAISettings) RekognitionRegion() string     { return f.region }
func (f fakeAISettings) RekognitionCollection() string { return f.collection }

// stubAIBuilders replaces the real Ollama/Rekognition constructors with fakes
// for the duration of a test, so registerAIWorkerHandlers can be tested offline.
// The fakes register a trivial handler under the right kind so HandlerKinds()
// reflects what production would register. faceErr, when non-nil, simulates an
// AWS init/ensure failure so the graceful-degradation path can be asserted.
func stubAIBuilders(t *testing.T, faceErr error) {
	t.Helper()
	origEnrich, origFace := registerEnrichHandler, registerFaceHandler
	t.Cleanup(func() { registerEnrichHandler, registerFaceHandler = origEnrich, origFace })

	registerEnrichHandler = func(w *worker.Worker, _ string) {
		w.RegisterHandler(catalog.JobKindEnrichAI, func(context.Context, worker.JobView, string) (worker.HandlerResult, error) {
			return worker.HandlerResult{}, nil
		})
	}
	registerFaceHandler = func(_ context.Context, w *worker.Worker, _ aiSettings) error {
		if faceErr != nil {
			return faceErr
		}
		w.RegisterHandler(catalog.JobKindFaceIndex, func(context.Context, worker.JobView, string) (worker.HandlerResult, error) {
			return worker.HandlerResult{}, nil
		})
		return nil
	}
}

func newTestWorker() *worker.Worker {
	return worker.New(nil, nil, worker.Options{})
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestRegisterAIWorkerHandlersFullConfig: with both [ai] ollama_url and
// rekognition_collection set, registerAIWorkerHandlers registers BOTH the
// enrich-ai and face-index worker handlers, so the worker will claim and
// dispatch both AI job kinds.
func TestRegisterAIWorkerHandlersFullConfig(t *testing.T) {
	stubAIBuilders(t, nil)
	w := newTestWorker()

	registerAIWorkerHandlers(context.Background(), w, fakeAISettings{
		ollamaURL:  "http://gpu-box:11434",
		profile:    "imyousuf",
		region:     "us-east-1",
		collection: "faces",
	}, quietLogger())

	assertKinds(t, w.HandlerKinds(), catalog.JobKindEnrichAI, catalog.JobKindFaceIndex)
}

// TestRegisterAIWorkerHandlersAbsentConfig: with no [ai] config, NOTHING is
// registered — the worker runs transform kinds only, cleanly (no panic, no
// network).
func TestRegisterAIWorkerHandlersAbsentConfig(t *testing.T) {
	stubAIBuilders(t, nil)
	w := newTestWorker()

	registerAIWorkerHandlers(context.Background(), w, fakeAISettings{}, quietLogger())

	if got := w.HandlerKinds(); len(got) != 0 {
		t.Fatalf("absent config registered handlers %v, want none", got)
	}
}

// TestRegisterAIWorkerHandlersOllamaOnly: ollama_url set but no collection ->
// only enrich-ai is registered; face-index is skipped.
func TestRegisterAIWorkerHandlersOllamaOnly(t *testing.T) {
	stubAIBuilders(t, nil)
	w := newTestWorker()

	registerAIWorkerHandlers(context.Background(), w, fakeAISettings{ollamaURL: "http://gpu-box:11434"}, quietLogger())

	assertKinds(t, w.HandlerKinds(), catalog.JobKindEnrichAI)
	if hasKind(w.HandlerKinds(), catalog.JobKindFaceIndex) {
		t.Errorf("face-index registered with no collection: %v", w.HandlerKinds())
	}
}

// TestRegisterAIWorkerHandlersFaceInitFailureDegrades: a collection is
// configured but Rekognition init/ensure fails (e.g. no AWS creds). The
// face-index handler is NOT registered, the worker does not crash, and the
// still-valid enrich-ai handler IS registered — graceful degradation.
func TestRegisterAIWorkerHandlersFaceInitFailureDegrades(t *testing.T) {
	stubAIBuilders(t, errors.New("aws creds unavailable"))
	w := newTestWorker()

	registerAIWorkerHandlers(context.Background(), w, fakeAISettings{
		ollamaURL:  "http://gpu-box:11434",
		collection: "faces",
	}, quietLogger())

	assertKinds(t, w.HandlerKinds(), catalog.JobKindEnrichAI)
	if hasKind(w.HandlerKinds(), catalog.JobKindFaceIndex) {
		t.Errorf("face-index registered despite init failure: %v", w.HandlerKinds())
	}
}

func hasKind(kinds []string, want string) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

func assertKinds(t *testing.T, got []string, want ...string) {
	t.Helper()
	g := append([]string(nil), got...)
	sort.Strings(g)
	for _, w := range want {
		if !hasKind(g, w) {
			t.Errorf("kind %q missing from %v", w, g)
		}
	}
	if len(g) != len(want) {
		t.Errorf("registered kinds = %v, want exactly %v", g, want)
	}
}
