package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/jobs"
	"github.com/imyousuf/fs-image-manager/internal/transform"
)

// fakeServer is a stand-in internal job API: it hands out a fixed batch of jobs
// on the first claim, serves source bytes, and records posted results and the
// terminal complete/fail calls. It checks the shared secret on every request.
type fakeServer struct {
	mu sync.Mutex

	secret  string
	jobs    []jobs.JobView
	source  map[string][]byte // jobID -> source bytes
	claimed bool

	results    map[string][]byte // jobID -> uploaded derivative bytes
	resultKind map[string]string
	completed  map[string]bool
	failed     map[string]string
}

func newFakeServer(secret string) *fakeServer {
	return &fakeServer{
		secret:     secret,
		source:     map[string][]byte{},
		results:    map[string][]byte{},
		resultKind: map[string]string{},
		completed:  map[string]bool{},
		failed:     map[string]string{},
	}
}

func (s *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/jobs/claim", s.claim)
	mux.HandleFunc("GET /internal/jobs/{id}/source", s.serveSource)
	mux.HandleFunc("POST /internal/jobs/{id}/result", s.result)
	mux.HandleFunc("POST /internal/jobs/{id}/complete", s.complete)
	mux.HandleFunc("POST /internal/jobs/{id}/fail", s.fail)
	return s.auth(mux)
}

func (s *fakeServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *fakeServer) claim(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := jobs.ClaimResponse{}
	if !s.claimed {
		s.claimed = true
		out.Jobs = s.jobs
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *fakeServer) serveSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	b, ok := s.source[id]
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_, _ = w.Write(b)
}

func (s *fakeServer) result(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	kind := r.FormValue("kind")
	var data []byte
	if f, _, err := r.FormFile("file"); err == nil {
		defer func() { _ = f.Close() }()
		data, _ = io.ReadAll(f)
	}
	s.mu.Lock()
	s.results[id] = data
	s.resultKind[id] = kind
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (s *fakeServer) complete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	s.completed[id] = true
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (s *fakeServer) fail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body jobs.FailRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	s.failed[id] = body.Reason
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// fakeRunner is a transform.Runner that writes a canned derivative file.
type fakeRunner struct {
	kind string
	mime string
	body []byte
	err  error
}

func (f fakeRunner) Run(_ context.Context, req transform.Request) (transform.Result, error) {
	if f.err != nil {
		return transform.Result{}, f.err
	}
	out := req.OutputDir + string(os.PathSeparator) + "deriv.bin"
	if err := os.WriteFile(out, f.body, 0o600); err != nil {
		return transform.Result{}, err
	}
	return transform.Result{OutputPath: out, DerivativeKind: f.kind, Mime: f.mime}, nil
}

func TestWorkerProcessesTransformJob(t *testing.T) {
	fs := newFakeServer("tok")
	fs.jobs = []jobs.JobView{{
		ID: "job1", Kind: catalog.JobKindConvertImage, AssetID: "a",
		MediaPath: "pics/a.jpg", Params: map[string]string{"format": "webp"},
	}}
	fs.source["job1"] = []byte("SRC")

	ts := httptest.NewServer(fs.handler())
	defer ts.Close()

	client := NewClient(ts.URL, "tok", ts.Client())
	w := New(client, fakeRunner{kind: "webp", mime: "image/webp", body: []byte("DERIV")},
		Options{Concurrency: 2, PollInterval: 10 * time.Millisecond, TempDir: t.TempDir()})

	runWorkerUntil(t, w, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return fs.completed["job1"]
	})

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if string(fs.results["job1"]) != "DERIV" {
		t.Fatalf("derivative not uploaded: %q", string(fs.results["job1"]))
	}
	if fs.resultKind["job1"] != "webp" {
		t.Fatalf("derivative kind %q", fs.resultKind["job1"])
	}
	if !fs.completed["job1"] {
		t.Fatal("job not completed")
	}
}

func TestWorkerFailsOnRunnerError(t *testing.T) {
	fs := newFakeServer("tok")
	fs.jobs = []jobs.JobView{{ID: "job2", Kind: catalog.JobKindTranscodeVideo, MediaPath: "videos/a.mov"}}
	fs.source["job2"] = []byte("SRC")

	ts := httptest.NewServer(fs.handler())
	defer ts.Close()

	client := NewClient(ts.URL, "tok", ts.Client())
	w := New(client, fakeRunner{err: io.ErrUnexpectedEOF},
		Options{Concurrency: 1, PollInterval: 10 * time.Millisecond, TempDir: t.TempDir()})

	runWorkerUntil(t, w, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		_, ok := fs.failed["job2"]
		return ok
	})

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, ok := fs.completed["job2"]; ok {
		t.Fatal("errored job should not be completed")
	}
	if !strings.Contains(fs.failed["job2"], "EOF") {
		t.Fatalf("failure reason not propagated: %q", fs.failed["job2"])
	}
}

func TestWorkerDelegatesHandlerJob(t *testing.T) {
	fs := newFakeServer("tok")
	fs.jobs = []jobs.JobView{{ID: "job3", Kind: catalog.JobKindEnrichAI, MediaPath: "pics/a.jpg"}}
	fs.source["job3"] = []byte("SRC")

	ts := httptest.NewServer(fs.handler())
	defer ts.Close()

	var gotSource string
	client := NewClient(ts.URL, "tok", ts.Client())
	w := New(client, fakeRunner{}, Options{Concurrency: 1, PollInterval: 10 * time.Millisecond, TempDir: t.TempDir()})
	w.RegisterHandler(catalog.JobKindEnrichAI, func(_ context.Context, _ JobView, src string) (HandlerResult, error) {
		b, _ := os.ReadFile(src) //nolint:gosec // src is the worker's own staged temp file
		gotSource = string(b)
		return HandlerResult{Data: map[string]any{"caption": "x"}}, nil
	})

	runWorkerUntil(t, w, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return fs.completed["job3"]
	})

	if gotSource != "SRC" {
		t.Fatalf("handler did not receive staged source: %q", gotSource)
	}
}

func TestClaimKindsDefaultIncludesHandlers(t *testing.T) {
	w := New(nil, fakeRunner{}, Options{})
	w.RegisterHandler(catalog.JobKindEnrichAI, func(context.Context, JobView, string) (HandlerResult, error) {
		return HandlerResult{}, nil
	})
	kinds := w.claimKinds()
	has := map[string]bool{}
	for _, k := range kinds {
		has[k] = true
	}
	for _, want := range []string{
		catalog.JobKindTranscodeVideo, catalog.JobKindDevelopRAW,
		catalog.JobKindConvertImage, catalog.JobKindEnrichAI,
	} {
		if !has[want] {
			t.Errorf("default claim kinds missing %q: %v", want, kinds)
		}
	}
}

// runWorkerUntil runs the worker in a goroutine and cancels it once cond holds
// (or the deadline passes, failing the test).
func runWorkerUntil(t *testing.T, w *Worker, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			cancel()
			<-done
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("condition not met before deadline")
}
