package jobs

import (
	"context"
	"fmt"
	"sync"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// ResultHandler stores the outcome of a finished job. The serve process wires a
// real handler per job kind at startup (e.g. media-pipeline's cache for
// transcode/develop/convert; search-index/ai-people for enrich/face data), so
// internal/jobs never imports those packages — only this callback type. The
// HTTP API invokes the handler for a job's kind when the worker posts a result.
//
// The handler receives the completed job and its result payload (derivative
// Bytes + Mime and/or structured Data). It is responsible for persisting the
// derivative (via catalog.Cache) and/or the structured data, and must be safe
// for concurrent use across jobs.
type ResultHandler func(ctx context.Context, job catalog.Job, result catalog.JobResult) error

// Registry maps a job kind to the ResultHandler that persists its output. It is
// populated at serve startup and read by the internal API for each posted
// result. Lookups for an unregistered kind are not an error: the API still
// records terminal job state, it just performs no extra persistence (useful
// when a teammate's package is not yet wired).
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]ResultHandler
}

// NewRegistry returns an empty result-handler registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]ResultHandler)}
}

// Register associates h with the given job kind, replacing any prior handler.
// A nil handler clears the kind. Intended to be called only during startup
// wiring, before the server begins serving requests.
func (r *Registry) Register(kind string, h ResultHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h == nil {
		delete(r.handlers, kind)
		return
	}
	r.handlers[kind] = h
}

// Handler returns the handler registered for kind and whether one exists.
func (r *Registry) Handler(kind string) (ResultHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[kind]
	return h, ok
}

// Handle runs the registered handler for the job's kind, if any. A missing
// handler is a no-op (returns nil) so a worker can still complete a job whose
// persistence side is not yet wired.
func (r *Registry) Handle(ctx context.Context, job catalog.Job, result catalog.JobResult) error {
	h, ok := r.Handler(job.Kind)
	if !ok {
		return nil
	}
	if err := h(ctx, job, result); err != nil {
		return fmt.Errorf("jobs: result handler for %q: %w", job.Kind, err)
	}
	return nil
}
