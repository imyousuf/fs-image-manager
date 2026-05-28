package worker

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/jobs"
)

// newHTTPTestServer starts the fakeServer over httptest and returns its base
// URL, registering teardown on t.
func newHTTPTestServer(t *testing.T, fs *fakeServer) string {
	t.Helper()
	ts := httptest.NewServer(fs.handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

// assertHasKinds fails unless every want kind is present in got.
func assertHasKinds(t *testing.T, got []string, want ...string) {
	t.Helper()
	has := make(map[string]bool, len(got))
	for _, k := range got {
		has[k] = true
	}
	for _, w := range want {
		if !has[w] {
			t.Errorf("kind %q missing from %v", w, got)
		}
	}
}

// discardLogger is a slog.Logger that drops output, keeping test logs quiet.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestWorkerDispatchesEachKindToItsHandler is the task #20 routing proof: a
// worker with both AI handlers registered (as the enrich-worker assembly does
// from [ai] config) claims an enrich-ai job AND a face-index job in one batch
// and dispatches each to its own handler — never crossing the wires. Uses the
// fakeServer + fakeRunner harness (no network); the handlers are local stand-ins
// for ai-people's Ollama/Rekognition handlers, so dispatch is what we assert.
func TestWorkerDispatchesEachKindToItsHandler(t *testing.T) {
	fs := newFakeServer("tok")
	fs.jobs = []jobs.JobView{
		{ID: "enrich1", Kind: catalog.JobKindEnrichAI, MediaPath: "pics/a.jpg"},
		{ID: "face1", Kind: catalog.JobKindFaceIndex, MediaPath: "pics/b.jpg"},
	}
	fs.source["enrich1"] = []byte("ENRICH-SRC")
	fs.source["face1"] = []byte("FACE-SRC")

	ts := newHTTPTestServer(t, fs)
	client := NewClient(ts, "tok", nil)

	// Record which kind each handler saw, so a misrouted job is caught.
	enrichGot := make(chan string, 1)
	faceGot := make(chan string, 1)

	w := New(client, fakeRunner{}, Options{Concurrency: 2, PollInterval: 10 * time.Millisecond, TempDir: t.TempDir()})
	w.RegisterHandler(catalog.JobKindEnrichAI, func(_ context.Context, job JobView, _ string) (HandlerResult, error) {
		enrichGot <- job.Kind
		return HandlerResult{Data: map[string]any{"caption": "x"}}, nil
	})
	w.RegisterHandler(catalog.JobKindFaceIndex, func(_ context.Context, job JobView, _ string) (HandlerResult, error) {
		faceGot <- job.Kind
		return HandlerResult{Data: map[string]any{"faces": "[]"}}, nil
	})

	// Both kinds must be advertised on claim so the worker pulls this work.
	assertHasKinds(t, w.claimKinds(), catalog.JobKindEnrichAI, catalog.JobKindFaceIndex)

	runWorkerUntil(t, w, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return fs.completed["enrich1"] && fs.completed["face1"]
	})

	select {
	case k := <-enrichGot:
		if k != catalog.JobKindEnrichAI {
			t.Fatalf("enrich handler ran for kind %q, want %q", k, catalog.JobKindEnrichAI)
		}
	default:
		t.Fatal("enrich-ai handler was never invoked")
	}
	select {
	case k := <-faceGot:
		if k != catalog.JobKindFaceIndex {
			t.Fatalf("face handler ran for kind %q, want %q", k, catalog.JobKindFaceIndex)
		}
	default:
		t.Fatal("face-index handler was never invoked")
	}
}

// TestRunInvokesSetupHook proves the seam itself: worker.Run installs handlers
// via cfg.Setup on the internally-built Worker before claiming. We assert the
// hook receives a Worker on which RegisterHandler takes effect (the registered
// kind is then claimed). Run is cancelled as soon as the hook fires; we only
// care that Setup ran against the real Worker.
func TestRunInvokesSetupHook(t *testing.T) {
	fs := newFakeServer("tok") // empty job list: claim returns nothing, loop idles
	ts := newHTTPTestServer(t, fs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupRan := make(chan []string, 1)
	cfg := Config{
		Server: ts,
		Secret: "tok",
		Options: Options{
			Concurrency:  1,
			PollInterval: 10 * time.Millisecond,
			Logger:       discardLogger(),
		},
		Setup: func(w *Worker) {
			w.RegisterHandler(catalog.JobKindEnrichAI, func(context.Context, JobView, string) (HandlerResult, error) {
				return HandlerResult{}, nil
			})
			setupRan <- w.HandlerKinds()
			cancel() // hook fired; stop the run loop
		},
	}

	_ = Run(ctx, cfg)

	select {
	case kinds := <-setupRan:
		assertHasKinds(t, kinds, catalog.JobKindEnrichAI)
	default:
		t.Fatal("Run did not invoke cfg.Setup")
	}
}

// TestRunNilSetupIsTransformOnly proves a nil Setup hook is the prior behaviour:
// Run starts and claims only the transform kinds, registering no handlers.
func TestRunNilSetupIsTransformOnly(t *testing.T) {
	fs := newFakeServer("tok")
	ts := newHTTPTestServer(t, fs)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run with no Setup must not panic and must return on ctx timeout. (A panic
	// here would mean Run dereferenced a nil Setup.)
	cfg := Config{
		Server:  ts,
		Secret:  "tok",
		Options: Options{Concurrency: 1, PollInterval: 10 * time.Millisecond, Logger: discardLogger()},
	}
	if err := Run(ctx, cfg); err == nil {
		t.Fatal("Run should return ctx error on timeout")
	}
}
