// Package worker is the relocatable enrich-worker: a long-running client that
// pulls jobs from a serve instance's internal job API, runs the heavy transform
// (or delegates enrich/face jobs to pluggable ai-people handlers) on the GPU
// box, and posts the derivative/result back. It is the consumer half of the
// durable queue in internal/jobs; the two never run in the same process in
// production (serve has no GPU, the worker has no library).
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/jobs"
)

// Client talks to a serve instance's internal job API over HTTP, authenticating
// every request with the shared worker secret. It is safe for concurrent use.
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// NewClient builds a Client for the given serve base URL (e.g.
// "https://media-server:8080") and shared secret. The base URL's trailing
// slash, if any, is trimmed.
func NewClient(baseURL, secret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		// Generous timeout: source fetches and result uploads can be large.
		httpClient = &http.Client{Timeout: 30 * time.Minute}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    httpClient,
	}
}

func (c *Client) url(path string) string { return c.baseURL + "/internal/jobs" + path }

// auth stamps the shared-secret bearer header on a request.
func (c *Client) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.secret)
}

// Claim leases up to n jobs of the given kinds with the given lease duration.
func (c *Client) Claim(ctx context.Context, kinds []string, lease time.Duration, n int) ([]jobs.JobView, error) {
	body, err := json.Marshal(jobs.ClaimRequest{
		Kinds:        kinds,
		LeaseSeconds: int(lease / time.Second),
		N:            n,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/claim"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.auth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("worker: claim: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("worker: claim: %s", statusError(resp))
	}
	var cr jobs.ClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("worker: decode claim response: %w", err)
	}
	return cr.Jobs, nil
}

// FetchSource downloads the source bytes for a job to a new temp file under
// dir, returning the file path. The caller owns removing it.
func (c *Client) FetchSource(ctx context.Context, jobID, dir, baseName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/"+url.PathEscape(jobID)+"/source"), nil)
	if err != nil {
		return "", err
	}
	c.auth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("worker: fetch source: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("worker: fetch source: %s", statusError(resp))
	}

	name := baseName
	if name == "" {
		name = "source"
	}
	dst := dir + string(os.PathSeparator) + sanitizeName(name)
	f, err := os.Create(dst) //nolint:gosec // dst is inside the worker's own temp dir
	if err != nil {
		return "", fmt.Errorf("worker: create source file: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("worker: write source file: %w", err)
	}
	return dst, nil
}

// PostResultFile uploads a derivative file plus metadata for a job via
// multipart/form-data. A nil/empty path posts a data-only result.
func (c *Client) PostResultFile(ctx context.Context, jobID, filePath, derivKind, mime string, data map[string]any) error {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// Stream the multipart body so a large derivative is not buffered in memory.
	go func() {
		err := writeResultMultipart(mw, filePath, derivKind, mime, data)
		_ = mw.Close()
		_ = pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/"+url.PathEscape(jobID)+"/result"), pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	c.auth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("worker: post result: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("worker: post result: %s", statusError(resp))
	}
	return nil
}

// Complete marks a job completed, optionally persisting structured data.
func (c *Client) Complete(ctx context.Context, jobID string, data map[string]any) error {
	var body io.Reader
	contentType := ""
	if len(data) > 0 {
		b, err := json.Marshal(map[string]any{"data": data})
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		contentType = "application/json"
	}
	return c.post(ctx, "/"+url.PathEscape(jobID)+"/complete", contentType, body)
}

// Fail marks a job failed with a reason (the server applies retry/backoff).
func (c *Client) Fail(ctx context.Context, jobID, reason string) error {
	b, err := json.Marshal(jobs.FailRequest{Reason: reason})
	if err != nil {
		return err
	}
	return c.post(ctx, "/"+url.PathEscape(jobID)+"/fail", "application/json", bytes.NewReader(b))
}

func (c *Client) post(ctx context.Context, path, contentType string, body io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("worker: post %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("worker: post %s: %s", path, statusError(resp))
	}
	return nil
}

// writeResultMultipart writes the metadata fields and (optionally) the file
// part into mw. It is the body-producing half of PostResultFile.
func writeResultMultipart(mw *multipart.Writer, filePath, derivKind, mime string, data map[string]any) error {
	if derivKind != "" {
		if err := mw.WriteField("kind", derivKind); err != nil {
			return err
		}
	}
	if mime != "" {
		if err := mw.WriteField("mime", mime); err != nil {
			return err
		}
	}
	if len(data) > 0 {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if err := mw.WriteField("data", string(b)); err != nil {
			return err
		}
	}
	if filePath == "" {
		return nil
	}
	f, err := os.Open(filePath) //nolint:gosec // filePath is a derivative the worker just produced
	if err != nil {
		return fmt.Errorf("open derivative: %w", err)
	}
	defer func() { _ = f.Close() }()
	part, err := mw.CreateFormFile("file", sanitizeName(filePathBase(filePath)))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("copy derivative: %w", err)
	}
	return nil
}

// statusError reads up to 1 KiB of an error response body for the message.
func statusError(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return resp.Status
	}
	return resp.Status + ": " + msg
}
