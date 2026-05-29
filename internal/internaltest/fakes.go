package internaltest

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// The fakes below implement the cross-cutting port interfaces declared in
// internal/catalog (the seams from docs/specs/_contracts.md §4): Cache, Queue,
// Enricher and FaceRecognizer. They are in-memory and configurable so tests
// can inject behaviour and assert interactions. Compile-time assertions below
// keep them honest drop-in mocks for the real implementations.
var (
	_ catalog.Cache          = (*FakeCache)(nil)
	_ catalog.Queue          = (*FakeQueue)(nil)
	_ catalog.Enricher       = (*FakeEnricher)(nil)
	_ catalog.FaceRecognizer = (*FakeFaceRecognizer)(nil)
)

// --- Cache (catalog.Cache; implemented by media-pipeline's internal/cache) ---

// FakeCache is an in-memory catalog.Cache. Put stores bytes under a key; Get
// returns a synthetic path; the bytes are retrievable via Bytes for assertion.
type FakeCache struct {
	mu      sync.Mutex
	entries map[string]fakeCacheEntry
}

type fakeCacheEntry struct {
	data []byte
	mime string
}

// NewFakeCache returns an empty in-memory cache.
func NewFakeCache() *FakeCache {
	return &FakeCache{entries: make(map[string]fakeCacheEntry)}
}

// Get reports whether key is present and returns its synthetic path.
func (c *FakeCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok {
		return "", false
	}
	return "mem://" + key, true
}

// Put stores r's bytes under key and returns its synthetic path.
func (c *FakeCache) Put(key string, r io.Reader, mime string) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = fakeCacheEntry{data: data, mime: mime}
	return "mem://" + key, nil
}

// Key composes a deterministic cache key from its parts.
func (c *FakeCache) Key(assetID, kind, params, srcHash string) string {
	return fmt.Sprintf("%s|%s|%s|%s", assetID, kind, params, srcHash)
}

// Bytes returns the stored bytes (and mime) for key, for test assertions.
func (c *FakeCache) Bytes(key string) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e.data, e.mime, ok
}

// --- Queue (catalog.Queue; implemented by jobs-worker's internal/jobs) ---

// FakeQueue is an in-memory job queue that records enqueues and hands them back
// on Claim. It is intentionally simple (no leasing/backoff) — enough to test
// producers and consumers in isolation.
type FakeQueue struct {
	mu       sync.Mutex
	seq      int
	jobs     []catalog.Job
	complete map[string]catalog.JobResult
	failed   map[string]string
}

// NewFakeQueue returns an empty in-memory queue.
func NewFakeQueue() *FakeQueue {
	return &FakeQueue{
		complete: make(map[string]catalog.JobResult),
		failed:   make(map[string]string),
	}
}

// Enqueue records a new pending job and returns it.
func (q *FakeQueue) Enqueue(_ context.Context, kind, assetID string, mp catalog.MediaPath, params map[string]string) (catalog.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	job := catalog.Job{
		ID:        fmt.Sprintf("job-%d", q.seq),
		Kind:      kind,
		AssetID:   assetID,
		MediaPath: mp,
		Params:    params,
		Status:    "pending",
	}
	q.jobs = append(q.jobs, job)
	return job, nil
}

// Claim returns up to n pending jobs matching kinds, marking them claimed.
func (q *FakeQueue) Claim(_ context.Context, kinds []string, _ time.Duration, n int) ([]catalog.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	want := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	var claimed []catalog.Job
	for i := range q.jobs {
		if len(claimed) >= n {
			break
		}
		if q.jobs[i].Status != "pending" {
			continue
		}
		if len(want) > 0 && !want[q.jobs[i].Kind] {
			continue
		}
		q.jobs[i].Status = "claimed"
		claimed = append(claimed, q.jobs[i])
	}
	return claimed, nil
}

// Complete records a successful result for job id.
func (q *FakeQueue) Complete(_ context.Context, id string, result catalog.JobResult) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.complete[id] = result
	q.markStatus(id, "completed")
	return nil
}

// Fail records a failure reason for job id.
func (q *FakeQueue) Fail(_ context.Context, id, reason string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed[id] = reason
	q.markStatus(id, "failed")
	return nil
}

func (q *FakeQueue) markStatus(id, status string) {
	for i := range q.jobs {
		if q.jobs[i].ID == id {
			q.jobs[i].Status = status
			return
		}
	}
}

// Pending returns a snapshot of jobs still pending, for assertions.
func (q *FakeQueue) Pending() []catalog.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []catalog.Job
	for _, j := range q.jobs {
		if j.Status == "pending" {
			out = append(out, j)
		}
	}
	return out
}

// --- Enricher (catalog.Enricher; implemented by ai-people's internal/enrich) ---

// FakeEnricher returns a fixed EnrichResult (or Err) and records each call.
type FakeEnricher struct {
	Result catalog.EnrichResult
	Err    error

	mu    sync.Mutex
	calls []catalog.MediaPath
}

// Enrich returns the configured result and records the media path.
func (e *FakeEnricher) Enrich(_ context.Context, mp catalog.MediaPath, _ io.Reader) (catalog.EnrichResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, mp)
	e.mu.Unlock()
	if e.Err != nil {
		return catalog.EnrichResult{}, e.Err
	}
	return e.Result, nil
}

// Calls returns the media paths Enrich was called with.
func (e *FakeEnricher) Calls() []catalog.MediaPath {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]catalog.MediaPath, len(e.calls))
	copy(out, e.calls)
	return out
}

// --- FaceRecognizer (catalog.FaceRecognizer; implemented by ai-people) ---

// FakeFaceRecognizer returns canned detection/match results.
type FakeFaceRecognizer struct {
	Faces      []catalog.Face
	DetectErr  error
	PersonID   string
	Confidence float64
	MatchErr   error
}

// DetectAndEmbed returns the configured faces.
func (f *FakeFaceRecognizer) DetectAndEmbed(_ context.Context, _ io.Reader) ([]catalog.Face, error) {
	if f.DetectErr != nil {
		return nil, f.DetectErr
	}
	return f.Faces, nil
}

// Match returns the configured person id and confidence.
func (f *FakeFaceRecognizer) Match(_ context.Context, _ catalog.Face) (string, float64, error) {
	if f.MatchErr != nil {
		return "", 0, f.MatchErr
	}
	return f.PersonID, f.Confidence, nil
}
