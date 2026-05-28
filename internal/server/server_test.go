package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/httpx"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// testFrontend builds an in-memory SPA with an index.html and one real asset.
func testFrontend() fs.FS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<html>SPA ROOT</html>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
		"favicon.ico":   {Data: []byte("icon")},
	}
}

// newTestServer returns a server and an httptest server wrapping its handler.
func newTestServer(t *testing.T, opts Options) (*Server, *httptest.Server) {
	t.Helper()
	if opts.FrontendFS == nil {
		opts.FrontendFS = testFrontend()
	}
	s := New(opts)
	ts := httptest.NewServer(s.handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestAPIOpenWhenNoToken(t *testing.T) {
	s, ts := newTestServer(t, Options{})
	s.API().HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"pong": "ok"})
	})

	resp, err := http.Get(ts.URL + "/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestAPIRequiresTokenWhenSet(t *testing.T) {
	s, ts := newTestServer(t, Options{AuthToken: "s3cr3t"})
	s.API().HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"pong": "ok"})
	})

	t.Run("no token rejected", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/ping")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		// Error envelope shape check.
		var env httpx.ErrorEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Error.Code != "unauthorized" {
			t.Errorf("error code = %q, want unauthorized", env.Error.Code)
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/ping", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("correct token accepted", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/ping", nil)
		req.Header.Set("Authorization", "Bearer s3cr3t")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestInternalAPISecret(t *testing.T) {
	s, ts := newTestServer(t, Options{WorkerSecret: "worker-secret"})
	s.Internal().HandleFunc("POST /claim", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"claimed": "0"})
	})

	t.Run("rejected without secret", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/internal/jobs/claim", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("accepted with bearer secret", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/internal/jobs/claim", nil)
		req.Header.Set("Authorization", "Bearer worker-secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("accepted with X-Worker-Secret header", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/internal/jobs/claim", nil)
		req.Header.Set("X-Worker-Secret", "worker-secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	})
}

func TestInternalAPIDisabledWithoutSecret(t *testing.T) {
	// No WorkerSecret => the internal group isn't mounted; requests fall through
	// to the SPA handler and get the index.html (200), never the job handler.
	s, ts := newTestServer(t, Options{})
	called := false
	s.Internal().HandleFunc("POST /claim", func(http.ResponseWriter, *http.Request) {
		called = true
	})
	resp, err := http.Post(ts.URL+"/internal/jobs/claim", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if called {
		t.Error("internal handler should not be reachable without a worker secret")
	}
}

func TestSPAServesRealFile(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	resp, err := http.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readAll(t, resp)
	if !strings.Contains(body, "console.log") {
		t.Errorf("expected real asset content, got %q", body)
	}
}

func TestSPAFallbackToIndex(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	// A deep client-side route that is not a real file must serve index.html.
	resp, err := http.Get(ts.URL + "/library/pictures/2021")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)
	if !strings.Contains(body, "SPA ROOT") {
		t.Errorf("SPA fallback did not serve index.html, got %q", body)
	}
}

func TestSPARootServesIndex(t *testing.T) {
	_, ts := newTestServer(t, Options{})
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readAll(t, resp)
	if !strings.Contains(body, "SPA ROOT") {
		t.Errorf("root did not serve index.html, got %q", body)
	}
}

func TestNoFrontend404(t *testing.T) {
	s := New(Options{}) // FrontendFS nil
	ts := httptest.NewServer(s.handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/anything")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var env httpx.ErrorEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", env.Error.Code)
	}
}

func TestPanicRecovery(t *testing.T) {
	s, ts := newTestServer(t, Options{})
	s.API().HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})
	resp, err := http.Get(ts.URL + "/api/boom")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var env httpx.ErrorEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Error.Code != "internal" {
		t.Errorf("code = %q, want internal", env.Error.Code)
	}
}

// TestTraversalRejectedThroughHTTP is the end-to-end acceptance check: a
// resolver-backed handler must reject "pictures/../../etc/passwd" with a 400
// invalid_path envelope, exercising the full HTTP -> mediapath -> envelope
// path that every media handler will use.
func TestTraversalRejectedThroughHTTP(t *testing.T) {
	resolver, err := mediapath.NewResolver([]catalog.Library{
		{Alias: "pictures", Name: "Pictures", Root: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	s, ts := newTestServer(t, Options{Resolver: resolver})
	// Register a handler that resolves the ?path= input via the resolver,
	// mirroring how real media handlers parse MediaPath inputs.
	s.API().HandleFunc("GET /open", func(w http.ResponseWriter, r *http.Request) {
		mp, err := httpx.ParseMediaPath(s.Resolver(), r.URL.Query().Get("path"))
		if err != nil {
			httpx.WriteAPIError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"resolved": string(mp)})
	})

	cases := []struct {
		path       string
		wantStatus int
		wantCode   string
	}{
		{"pictures/../../etc/passwd", http.StatusBadRequest, "invalid_path"},
		{"/etc/passwd", http.StatusBadRequest, "invalid_path"},
		{"nope/x.jpg", http.StatusNotFound, "unknown_alias"},
		{"pictures/2021/a.jpg", http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + "/api/open?path=" + url.QueryEscape(tc.path))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantCode != "" {
				var env httpx.ErrorEnvelope
				if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
					t.Fatal(err)
				}
				if env.Error.Code != tc.wantCode {
					t.Errorf("code = %q, want %q", env.Error.Code, tc.wantCode)
				}
			}
		})
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
