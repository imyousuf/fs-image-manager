package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/jobs"
	"github.com/imyousuf/fs-image-manager/internal/transform"
)

// Handler runs a non-transform job (enrich-ai / face-index) that the worker
// delegates rather than running through transform. ai-people registers its
// Enricher/FaceRecognizer-backed handlers here so the worker stays decoupled
// from those packages. The handler is given the staged source path and returns
// the result to post back (derivative file path optional, structured data
// optional).
type Handler func(ctx context.Context, job JobView, sourcePath string) (HandlerResult, error)

// JobView re-exports the claimed-job wire shape so handler authors need not
// import internal/jobs directly.
type JobView = jobs.JobView

// HandlerResult is what a delegated Handler produces.
type HandlerResult struct {
	// FilePath is an optional derivative file to upload (empty = data-only).
	FilePath string
	// DerivativeKind/Mime describe the derivative when FilePath is set.
	DerivativeKind string
	Mime           string
	// Data is optional structured output (labels, faces, embedding refs, ...).
	Data map[string]any
}

// Options configures a Worker.
type Options struct {
	// Kinds the worker will claim; empty means all kinds it can handle.
	Kinds []string
	// Concurrency caps simultaneously-running jobs (>=1).
	Concurrency int
	// BatchSize is how many jobs to claim per poll (defaults to Concurrency).
	BatchSize int
	// Lease is the visibility timeout requested per claim.
	Lease time.Duration
	// PollInterval is the base sleep between polls when the queue is empty;
	// backoff grows it up to MaxPollInterval after repeated empties/errors.
	PollInterval    time.Duration
	MaxPollInterval time.Duration
	// TempDir is the parent for per-job scratch dirs (defaults to os.TempDir).
	TempDir string
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

func (o *Options) applyDefaults() {
	if o.Concurrency < 1 {
		o.Concurrency = 2
	}
	if o.BatchSize < 1 {
		o.BatchSize = o.Concurrency
	}
	if o.Lease <= 0 {
		o.Lease = 10 * time.Minute
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 2 * time.Second
	}
	if o.MaxPollInterval <= 0 {
		o.MaxPollInterval = 30 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// Worker is the claim→process→report loop. It runs transform jobs via the
// injected Runner and delegates other kinds to registered Handlers.
type Worker struct {
	client  *Client
	runner  transform.Runner
	opts    Options
	log     *slog.Logger
	tempDir string

	mu       sync.RWMutex
	handlers map[string]Handler
}

// New builds a Worker over a Client and a transform.Runner. opts defaults are
// filled in.
func New(client *Client, runner transform.Runner, opts Options) *Worker {
	opts.applyDefaults()
	tmp := opts.TempDir
	if tmp == "" {
		tmp = os.TempDir()
	}
	return &Worker{
		client:   client,
		runner:   runner,
		opts:     opts,
		log:      opts.Logger,
		tempDir:  tmp,
		handlers: make(map[string]Handler),
	}
}

// RegisterHandler installs a delegate for a non-transform kind (e.g.
// catalog.JobKindEnrichAI). Call before Run.
func (w *Worker) RegisterHandler(kind string, h Handler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[kind] = h
}

// HandlerKinds returns the non-transform kinds that currently have a registered
// handler (e.g. enrich-ai, face-index). Order is unspecified. It lets the
// assembly layer and tests confirm which delegated kinds the worker will claim
// without reaching into the worker's internals.
func (w *Worker) HandlerKinds() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	kinds := make([]string, 0, len(w.handlers))
	for k := range w.handlers {
		kinds = append(kinds, k)
	}
	return kinds
}

// claimKinds is the set of kinds the worker advertises on claim: the configured
// Kinds, or — if none configured — the transform kinds plus any registered
// handler kinds (so it never claims work it cannot run).
func (w *Worker) claimKinds() []string {
	if len(w.opts.Kinds) > 0 {
		return w.opts.Kinds
	}
	kinds := []string{
		catalog.JobKindTranscodeVideo,
		catalog.JobKindDevelopRAW,
		catalog.JobKindConvertImage,
	}
	w.mu.RLock()
	for k := range w.handlers {
		kinds = append(kinds, k)
	}
	w.mu.RUnlock()
	return kinds
}

// Run is the worker loop: poll for jobs, process a batch concurrently, then
// sleep with backoff when idle. It returns when ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	kinds := w.claimKinds()
	backoff := w.opts.PollInterval
	w.log.Info("worker started",
		"kinds", kinds, "concurrency", w.opts.Concurrency, "batch", w.opts.BatchSize)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		claimed, err := w.client.Claim(ctx, kinds, w.opts.Lease, w.opts.BatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.log.Warn("worker claim failed", "err", err, "retry_in", backoff)
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff, w.opts.MaxPollInterval)
			continue
		}
		if len(claimed) == 0 {
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff, w.opts.MaxPollInterval)
			continue
		}
		// Got work: reset backoff and process the batch with bounded concurrency.
		backoff = w.opts.PollInterval
		w.processBatch(ctx, claimed)
	}
}

// processBatch runs the claimed jobs concurrently, bounded by Concurrency.
func (w *Worker) processBatch(ctx context.Context, batch []JobView) {
	sem := make(chan struct{}, w.opts.Concurrency)
	var wg sync.WaitGroup
	for _, job := range batch {
		wg.Add(1)
		sem <- struct{}{}
		go func(job JobView) {
			defer wg.Done()
			defer func() { <-sem }()
			w.processOne(ctx, job)
		}(job)
	}
	wg.Wait()
}

// processOne runs a single job end to end: stage source, transform/delegate,
// post result, then complete or fail. Any error fails the job (the server
// applies retry/backoff). A failure to even report failure is only logged.
func (w *Worker) processOne(ctx context.Context, job JobView) {
	log := w.log.With("job", job.ID, "kind", job.Kind)
	scratch, err := os.MkdirTemp(w.tempDir, "fsim-job-")
	if err != nil {
		log.Error("worker: scratch dir", "err", err)
		w.failJob(ctx, job.ID, fmt.Sprintf("scratch dir: %v", err))
		return
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	srcPath, err := w.client.FetchSource(ctx, job.ID, scratch, baseNameFromPath(job.MediaPath))
	if err != nil {
		log.Error("worker: fetch source", "err", err)
		w.failJob(ctx, job.ID, fmt.Sprintf("fetch source: %v", err))
		return
	}

	res, err := w.process(ctx, job, scratch, srcPath)
	if err != nil {
		log.Error("worker: process", "err", err)
		w.failJob(ctx, job.ID, err.Error())
		return
	}

	if err := w.client.PostResultFile(ctx, job.ID, res.FilePath, res.DerivativeKind, res.Mime, res.Data); err != nil {
		log.Error("worker: post result", "err", err)
		w.failJob(ctx, job.ID, fmt.Sprintf("post result: %v", err))
		return
	}
	if err := w.client.Complete(ctx, job.ID, nil); err != nil {
		log.Error("worker: complete", "err", err)
		return
	}
	log.Info("worker: job done", "derivative", res.DerivativeKind)
}

// process dispatches a job to the transform runner or a registered handler and
// normalises the outcome to a HandlerResult.
func (w *Worker) process(ctx context.Context, job JobView, scratch, srcPath string) (HandlerResult, error) {
	if transform.IsTransformKind(job.Kind) {
		out, err := w.runner.Run(ctx, transform.Request{
			Kind:       job.Kind,
			SourcePath: srcPath,
			SourceName: baseNameFromPath(job.MediaPath),
			OutputDir:  scratch,
			Params:     job.Params,
		})
		if err != nil {
			return HandlerResult{}, err
		}
		return HandlerResult{
			FilePath:       out.OutputPath,
			DerivativeKind: out.DerivativeKind,
			Mime:           out.Mime,
		}, nil
	}

	w.mu.RLock()
	h, ok := w.handlers[job.Kind]
	w.mu.RUnlock()
	if !ok {
		return HandlerResult{}, fmt.Errorf("worker: no handler for kind %q", job.Kind)
	}
	return h(ctx, job, srcPath)
}

// failJob reports a job failure, logging if even that fails.
func (w *Worker) failJob(ctx context.Context, id, reason string) {
	// Use a short detached context so we can still report failure if the run
	// context was cancelled mid-job.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := w.client.Fail(rctx, id, reason); err != nil {
		w.log.Error("worker: report failure", "job", id, "err", err)
	}
}

// nextBackoff doubles d, capped at max.
func nextBackoff(d, max time.Duration) time.Duration {
	d *= 2
	if d > max {
		return max
	}
	return d
}

// sleep waits for d or until ctx is done; it returns false if ctx was done.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// baseNameFromPath returns the final path element of an alias-prefixed media
// path (used only as a filename hint for staged files).
func baseNameFromPath(mp string) string {
	if i := strings.LastIndexByte(mp, '/'); i >= 0 {
		return mp[i+1:]
	}
	return mp
}

// filePathBase returns the base name of an OS path.
func filePathBase(p string) string { return filepath.Base(p) }

// sanitizeName strips directory separators and other risky characters from a
// filename so a staged/derivative file name can never traverse out of the
// worker's own temp dir. It is defence-in-depth: these names are worker-local.
func sanitizeName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "source"
	}
	return name
}
