package enrich

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// Default Ollama model names. These are overridable via OllamaOptions so an
// operator can point at whatever vision/embedding models they have pulled.
const (
	defaultVisionModel = "llava" // multimodal caption/tags
	defaultEmbedModel  = "nomic-embed-text"
)

// defaultCaptionPrompt asks for a concise factual caption plus a short
// comma-separated tag list, which we split into labels. Kept terse so a small
// local VLM stays on-task and the output is cheap to parse.
const defaultCaptionPrompt = "Describe this photo in one concise factual sentence, " +
	"then on a new line list 5 to 10 short comma-separated tags for its main " +
	"subjects, scene and setting. Do not add commentary."

// OllamaOptions configures an OllamaEnricher.
type OllamaOptions struct {
	// BaseURL is the Ollama server, e.g. "http://gpu-box:11434" ([ai] ollama_url).
	BaseURL string
	// VisionModel produces the caption/tags (defaults to "llava").
	VisionModel string
	// EmbedModel produces the semantic embedding (defaults to "nomic-embed-text").
	EmbedModel string
	// CaptionPrompt overrides the default caption/tags prompt.
	CaptionPrompt string
	// HTTPClient is used for all requests; nil installs a generous-timeout client
	// (vision inference on a large image can take a while).
	HTTPClient *http.Client
}

func (o *OllamaOptions) applyDefaults() {
	if o.VisionModel == "" {
		o.VisionModel = defaultVisionModel
	}
	if o.EmbedModel == "" {
		o.EmbedModel = defaultEmbedModel
	}
	if o.CaptionPrompt == "" {
		o.CaptionPrompt = defaultCaptionPrompt
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
}

// OllamaEnricher is a catalog.Enricher backed by a private Ollama HTTP server.
// It runs a multimodal model for a caption + tags, then an embedding model over
// the caption for the semantic-search vector. All inference is local to the
// user's network (no cloud); it is opt-in (NoopEnricher is the default).
//
// It is safe for concurrent use (the http.Client is, and it holds no per-call
// mutable state).
type OllamaEnricher struct {
	opts OllamaOptions
}

// NewOllamaEnricher builds an OllamaEnricher. BaseURL must be set (the caller
// only constructs this when [ai] ollama_url is configured); model names and the
// HTTP client default if unset.
func NewOllamaEnricher(opts OllamaOptions) *OllamaEnricher {
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	opts.applyDefaults()
	return &OllamaEnricher{opts: opts}
}

// Enrich reads the media bytes, asks the vision model for a caption + tags, then
// embeds the caption. A failure of either stage is returned to the caller (the
// job is retried); a partial result (caption but no embedding, say) is never
// silently shipped. The image is sent base64-encoded as Ollama expects.
func (e *OllamaEnricher) Enrich(ctx context.Context, _ catalog.MediaPath, r io.Reader) (catalog.EnrichResult, error) {
	img, err := io.ReadAll(r)
	if err != nil {
		return catalog.EnrichResult{}, fmt.Errorf("enrich: read media: %w", err)
	}
	if len(img) == 0 {
		return catalog.EnrichResult{}, fmt.Errorf("enrich: empty media stream")
	}

	caption, labels, err := e.caption(ctx, img)
	if err != nil {
		return catalog.EnrichResult{}, err
	}

	var embedding []float32
	if caption != "" {
		embedding, err = e.embed(ctx, caption)
		if err != nil {
			return catalog.EnrichResult{}, err
		}
	}

	return catalog.EnrichResult{
		Labels:    labels,
		Caption:   caption,
		Embedding: embedding,
	}, nil
}

// --- Ollama wire types -------------------------------------------------------

type generateRequest struct {
	Model  string   `json:"model"`
	Prompt string   `json:"prompt"`
	Images []string `json:"images,omitempty"` // base64-encoded
	Stream bool     `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embedResponse struct {
	Embedding []float64 `json:"embedding"`
}

// caption runs the vision model and parses its reply into a caption plus tags.
func (e *OllamaEnricher) caption(ctx context.Context, img []byte) (string, []string, error) {
	reqBody := generateRequest{
		Model:  e.opts.VisionModel,
		Prompt: e.opts.CaptionPrompt,
		Images: []string{base64.StdEncoding.EncodeToString(img)},
		Stream: false,
	}
	var resp generateResponse
	if err := e.postJSON(ctx, "/api/generate", reqBody, &resp); err != nil {
		return "", nil, fmt.Errorf("enrich: ollama generate: %w", err)
	}
	caption, labels := parseCaptionAndTags(resp.Response)
	return caption, labels, nil
}

// embed runs the embedding model over text and returns the vector as float32.
func (e *OllamaEnricher) embed(ctx context.Context, text string) ([]float32, error) {
	var resp embedResponse
	if err := e.postJSON(ctx, "/api/embeddings", embedRequest{Model: e.opts.EmbedModel, Prompt: text}, &resp); err != nil {
		return nil, fmt.Errorf("enrich: ollama embeddings: %w", err)
	}
	if len(resp.Embedding) == 0 {
		return nil, fmt.Errorf("enrich: ollama returned empty embedding")
	}
	out := make([]float32, len(resp.Embedding))
	for i, v := range resp.Embedding {
		out[i] = float32(v)
	}
	return out, nil
}

// postJSON marshals body, POSTs it to baseURL+path, and decodes the JSON reply
// into out. Non-2xx responses surface the (truncated) body as the error.
func (e *OllamaEnricher) postJSON(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.opts.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.opts.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// parseCaptionAndTags splits the VLM reply into a one-line caption and a tag
// list. The prompt asks for "caption\ntag, tag, ...", but small models drift, so
// this is forgiving: the first non-empty line is the caption; a later line that
// looks like a comma-separated list yields the tags. If no separate tag line is
// found, the caption stands alone with no tags (search still matches the caption
// text). Tags are trimmed, de-duplicated and lower-cased for stable FTS terms.
func parseCaptionAndTags(reply string) (string, []string) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "", nil
	}
	lines := strings.Split(reply, "\n")
	var caption string
	var tagLine string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if caption == "" {
			caption = strings.TrimPrefix(ln, "Caption:")
			caption = strings.TrimSpace(caption)
			continue
		}
		// First subsequent line that contains a comma is treated as the tag list.
		if strings.Contains(ln, ",") {
			tagLine = ln
			break
		}
	}
	tagLine = strings.TrimPrefix(strings.TrimSpace(tagLine), "Tags:")
	return caption, splitTags(tagLine)
}

// splitTags turns "Beach, sunset, two People" into ["beach","sunset","two people"],
// trimming, lower-casing and de-duplicating while preserving order.
func splitTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, raw := range strings.Split(s, ",") {
		t := strings.ToLower(strings.TrimSpace(raw))
		t = strings.Trim(t, ".;")
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
