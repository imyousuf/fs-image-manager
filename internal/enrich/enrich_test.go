package enrich_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/enrich"
)

// fakeSink records what Apply writes to search, standing in for *search.Index.
type fakeSink struct {
	mu        sync.Mutex
	labels    string
	caption   string
	ocr       string
	embedding []float32
	embedded  bool
}

func (s *fakeSink) UpsertEnrichmentText(_ context.Context, _, labels, caption, ocr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.labels, s.caption, s.ocr = labels, caption, ocr
	return nil
}

func (s *fakeSink) UpsertEmbedding(_ context.Context, _ string, vec []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedding = vec
	s.embedded = true
	return nil
}

func TestNoopEnricherReturnsEmpty(t *testing.T) {
	res, err := enrich.NoopEnricher{}.Enrich(context.Background(), "pictures/x.jpg", strings.NewReader("bytes"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if res.Caption != "" || len(res.Labels) != 0 || len(res.Embedding) != 0 {
		t.Fatalf("Noop should return empty result, got %+v", res)
	}
}

// TestApplyWritesTextAndEmbedding checks the search seam: labels join with a
// space, caption/ocr pass through, and a non-empty embedding is stored.
func TestApplyWritesTextAndEmbedding(t *testing.T) {
	sink := &fakeSink{}
	res := catalog.EnrichResult{
		Labels:    []string{"beach", "sunset"},
		Caption:   "two people on a beach at sunset",
		OCRText:   "",
		Embedding: []float32{0.1, 0.2, 0.3},
	}
	if err := enrich.Apply(context.Background(), sink, "asset-1", res); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sink.labels != "beach sunset" {
		t.Fatalf("labels = %q, want \"beach sunset\"", sink.labels)
	}
	if sink.caption != res.Caption {
		t.Fatalf("caption = %q", sink.caption)
	}
	if !sink.embedded || len(sink.embedding) != 3 {
		t.Fatalf("embedding not stored: %+v", sink.embedding)
	}
}

// TestApplyNoEmbeddingSkipsVectorStore confirms an empty embedding does not call
// the vector store (so NoopEnricher's output never writes a zero-dim row).
func TestApplyNoEmbeddingSkipsVectorStore(t *testing.T) {
	sink := &fakeSink{}
	if err := enrich.Apply(context.Background(), sink, "asset-1", catalog.EnrichResult{Caption: "hi"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sink.embedded {
		t.Fatalf("Apply should not store an empty embedding")
	}
}

// TestOllamaEnricherEnd2End runs the OllamaEnricher against a stub Ollama server,
// verifying it sends the image to /api/generate, parses the caption + tags, then
// embeds the caption via /api/embeddings. No real network.
func TestOllamaEnricherEnd2End(t *testing.T) {
	var gotGenerate, gotEmbed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/generate":
			gotGenerate = true
			var req struct {
				Images []string `json:"images"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.Images) != 1 || req.Images[0] == "" {
				t.Errorf("generate request missing base64 image: %+v", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": "A dog runs on a beach.\ndog, beach, running, sand",
			})
		case "/api/embeddings":
			gotEmbed = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embedding": []float64{0.5, 0.25, 0.125},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := enrich.NewOllamaEnricher(enrich.OllamaOptions{BaseURL: srv.URL})
	res, err := e.Enrich(context.Background(), "pictures/dog.jpg", strings.NewReader("fake-jpeg-bytes"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if !gotGenerate || !gotEmbed {
		t.Fatalf("expected both endpoints hit: generate=%v embed=%v", gotGenerate, gotEmbed)
	}
	if res.Caption != "A dog runs on a beach." {
		t.Fatalf("caption = %q", res.Caption)
	}
	wantTags := []string{"dog", "beach", "running", "sand"}
	if len(res.Labels) != len(wantTags) {
		t.Fatalf("labels = %v, want %v", res.Labels, wantTags)
	}
	for i := range wantTags {
		if res.Labels[i] != wantTags[i] {
			t.Fatalf("label[%d] = %q, want %q", i, res.Labels[i], wantTags[i])
		}
	}
	if len(res.Embedding) != 3 {
		t.Fatalf("embedding len = %d, want 3", len(res.Embedding))
	}
}

// TestOllamaEnricherEmptyMedia rejects an empty stream rather than calling the model.
func TestOllamaEnricherEmptyMedia(t *testing.T) {
	e := enrich.NewOllamaEnricher(enrich.OllamaOptions{BaseURL: "http://unused"})
	if _, err := e.Enrich(context.Background(), "x", strings.NewReader("")); err == nil {
		t.Fatalf("expected error on empty media")
	}
}
