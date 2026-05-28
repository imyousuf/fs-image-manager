package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
	"github.com/imyousuf/fs-image-manager/internal/server"
)

const testSecret = "s3cr3t-worker-token"

// apiFixture wires a queue + internal API onto a real platform server (so the
// shared-secret middleware and /internal/jobs prefix-strip are exercised
// end-to-end) backed by a temp library on disk. The server runs on a loopback
// ephemeral port via the production Start path.
type apiFixture struct {
	baseURL  string
	queue    *Queue
	registry *Registry
	libRoot  string
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()

	// Temp library root with one real source file.
	libRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(libRoot, "a.jpg"), []byte("SOURCE-BYTES"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := mediapath.NewResolver([]catalog.Library{{Alias: "pics", Name: "Pics", Root: libRoot}})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}

	conn, _ := openTestDB(t)
	q := NewQueue(conn)
	reg := NewRegistry()

	addr := freeAddr(t)
	srv := server.New(server.Options{
		Addr:         addr,
		WorkerSecret: testSecret,
		Resolver:     resolver,
		FrontendFS:   nil,
	})
	NewAPI(q, resolver, reg, nil).RegisterRoutes(srv.Internal())

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Start(ctx) }()
	t.Cleanup(cancel)

	baseURL := "http://" + addr
	waitReady(t, baseURL)
	return &apiFixture{baseURL: baseURL, queue: q, registry: reg, libRoot: libRoot}
}

// freeAddr reserves an ephemeral loopback port and returns its host:port. The
// listener is closed immediately; the brief reuse window is acceptable in a
// test.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// waitReady polls the server until it answers (any HTTP response) or times out.
func waitReady(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/internal/jobs/claim") //nolint:noctx // readiness probe
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("server at %s never became ready", baseURL)
}

func (f *apiFixture) do(t *testing.T, method, path, secret string, body io.Reader, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, f.baseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestAPIRequiresSecret(t *testing.T) {
	f := newAPIFixture(t)

	// No secret -> 401.
	resp := f.do(t, http.MethodPost, "/internal/jobs/claim", "", bytes.NewReader([]byte(`{"n":1}`)), "application/json")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-secret claim: want 401, got %d", resp.StatusCode)
	}

	// Wrong secret -> 401.
	resp2 := f.do(t, http.MethodPost, "/internal/jobs/claim", "wrong", bytes.NewReader([]byte(`{"n":1}`)), "application/json")
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong-secret claim: want 401, got %d", resp2.StatusCode)
	}
}

// TestAPIRoundTrip exercises the full claim -> source -> result -> complete
// path against the live internal API and asserts the registry handler receives
// the derivative.
func TestAPIRoundTrip(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()

	// Capture results delivered to the handler.
	var (
		mu      sync.Mutex
		handled []catalog.JobResult
	)
	f.registry.Register(catalog.JobKindConvertImage, func(_ context.Context, _ catalog.Job, r catalog.JobResult) error {
		mu.Lock()
		handled = append(handled, r)
		mu.Unlock()
		return nil
	})

	enq, err := f.queue.Enqueue(ctx, catalog.JobKindConvertImage, "a1", "pics/a.jpg", map[string]string{"format": "webp"})
	if err != nil {
		t.Fatal(err)
	}

	// 1) Claim.
	claimResp := f.do(t, http.MethodPost, "/internal/jobs/claim", testSecret,
		bytes.NewReader([]byte(`{"n":5,"leaseSeconds":60}`)), "application/json")
	defer func() { _ = claimResp.Body.Close() }()
	if claimResp.StatusCode != http.StatusOK {
		t.Fatalf("claim status %d", claimResp.StatusCode)
	}
	var cr ClaimResponse
	if err := json.NewDecoder(claimResp.Body).Decode(&cr); err != nil {
		t.Fatal(err)
	}
	if len(cr.Jobs) != 1 || cr.Jobs[0].ID != enq.ID {
		t.Fatalf("claim returned %+v", cr.Jobs)
	}
	if cr.Jobs[0].MediaPath != "pics/a.jpg" {
		t.Fatalf("claimed media path %q", cr.Jobs[0].MediaPath)
	}

	// 2) Source: should stream the on-disk bytes via the resolver.
	srcResp := f.do(t, http.MethodGet, "/internal/jobs/"+enq.ID+"/source", testSecret, nil, "")
	defer func() { _ = srcResp.Body.Close() }()
	if srcResp.StatusCode != http.StatusOK {
		t.Fatalf("source status %d", srcResp.StatusCode)
	}
	src, _ := io.ReadAll(srcResp.Body)
	if string(src) != "SOURCE-BYTES" {
		t.Fatalf("source body %q", string(src))
	}

	// 3) Result: upload a derivative via multipart.
	body, ct := multipartResult(t, "converted.webp", []byte("WEBP-DERIVATIVE"), "webp", "image/webp")
	resResp := f.do(t, http.MethodPost, "/internal/jobs/"+enq.ID+"/result", testSecret, body, ct)
	defer func() { _ = resResp.Body.Close() }()
	if resResp.StatusCode != http.StatusOK {
		drainLog(t, resResp)
		t.Fatalf("result status %d", resResp.StatusCode)
	}

	mu.Lock()
	gotHandled := append([]catalog.JobResult(nil), handled...)
	mu.Unlock()
	if len(gotHandled) != 1 {
		t.Fatalf("handler called %d times", len(gotHandled))
	}
	if string(gotHandled[0].Bytes) != "WEBP-DERIVATIVE" || gotHandled[0].DerivativeKind != "webp" {
		t.Fatalf("handler got %+v", gotHandled[0])
	}

	// 4) Complete.
	compResp := f.do(t, http.MethodPost, "/internal/jobs/"+enq.ID+"/complete", testSecret, nil, "")
	defer func() { _ = compResp.Body.Close() }()
	if compResp.StatusCode != http.StatusOK {
		t.Fatalf("complete status %d", compResp.StatusCode)
	}
	got, _ := f.queue.Get(ctx, enq.ID)
	if got.Status != statusCompleted {
		t.Fatalf("job not completed: %q", got.Status)
	}
}

// TestAPISourceTraversalReject ensures a job whose stored MediaPath escapes the
// library root cannot stream a file outside it: the resolver rejects it and the
// API returns a 4xx, never the bytes.
func TestAPISourceTraversalReject(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()

	// A secret file outside the library.
	outside := filepath.Join(filepath.Dir(f.libRoot), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Enqueue a job whose path tries to climb out of the library root. Enqueue
	// stores the raw path; the resolver validates only at open time.
	enq, err := f.queue.Enqueue(ctx, catalog.JobKindConvertImage, "a", "pics/../secret.txt", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp := f.do(t, http.MethodGet, "/internal/jobs/"+enq.ID+"/source", testSecret, nil, "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("traversal was NOT rejected: status %d", resp.StatusCode)
	}
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		t.Fatalf("traversal reject should be 4xx, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte("TOP-SECRET")) {
		t.Fatalf("leaked file contents on traversal!")
	}
}

func TestAPIFailReschedules(t *testing.T) {
	f := newAPIFixture(t)
	ctx := context.Background()
	enq, err := f.queue.Enqueue(ctx, catalog.JobKindConvertImage, "a", "pics/a.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.queue.Claim(ctx, nil, time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	resp := f.do(t, http.MethodPost, "/internal/jobs/"+enq.ID+"/fail", testSecret,
		bytes.NewReader([]byte(`{"reason":"ffmpeg crashed"}`)), "application/json")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fail status %d", resp.StatusCode)
	}
	got, _ := f.queue.Get(ctx, enq.ID)
	// One attempt of the default max -> rescheduled to pending, not failed.
	if got.Status != statusPending {
		t.Fatalf("after one fail want pending, got %q", got.Status)
	}
}

func multipartResult(t *testing.T, filename string, data []byte, kind, mime string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("kind", kind)
	_ = mw.WriteField("mime", mime)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func drainLog(t *testing.T, resp *http.Response) {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	t.Logf("response body: %s", string(b))
}
