package jobs

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/httpx"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// maxResultBytes caps an uploaded derivative payload. Derivatives (transcoded
// MP4s, developed JPGs, WebP/AVIF) can be large; this is generous but bounded
// so a misbehaving worker cannot exhaust memory.
const maxResultBytes = 2 << 30 // 2 GiB

// API is the internal job HTTP API the worker pulls from. It is mounted under
// the platform server's shared-secret-guarded /internal/jobs group via
// RegisterRoutes. The worker claims jobs, fetches each job's source bytes
// (resolver-validated — never raw filesystem paths), posts a derivative/result
// and finally marks the job complete or failed.
type API struct {
	queue    *Queue
	resolver mediapath.Resolver
	registry *Registry
	log      *slog.Logger
}

// NewAPI builds the internal job API over the queue, resolver and result-
// handler registry. A nil logger defaults to slog.Default().
func NewAPI(queue *Queue, resolver mediapath.Resolver, registry *Registry, log *slog.Logger) *API {
	if log == nil {
		log = slog.Default()
	}
	if registry == nil {
		registry = NewRegistry()
	}
	return &API{queue: queue, resolver: resolver, registry: registry, log: log}
}

// RegisterRoutes mounts the internal job endpoints on the server's internal
// mux. Register paths *without* the "/internal/jobs" prefix; the platform
// server strips it and wraps the group in shared-secret auth. Has no effect if
// the server was built without a worker secret.
//
// Routes:
//
//	POST /claim          -> lease jobs                 (ClaimRequest -> [JobView])
//	GET  /{id}/source    -> stream source bytes        (resolver-validated)
//	POST /{id}/result    -> store derivative + data    (multipart or raw bytes)
//	POST /{id}/complete  -> mark completed
//	POST /{id}/fail      -> mark failed (retry/backoff) (FailRequest)
func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /claim", a.handleClaim)
	mux.HandleFunc("GET /{id}/source", a.handleSource)
	mux.HandleFunc("POST /{id}/result", a.handleResult)
	mux.HandleFunc("POST /{id}/complete", a.handleComplete)
	mux.HandleFunc("POST /{id}/fail", a.handleFail)
}

// ClaimRequest is the body of POST /claim.
type ClaimRequest struct {
	// Kinds restricts the claim to these job kinds; empty means any kind.
	Kinds []string `json:"kinds"`
	// LeaseSeconds is the visibility timeout for the claimed jobs.
	LeaseSeconds int `json:"leaseSeconds"`
	// N is the maximum number of jobs to lease.
	N int `json:"n"`
}

// JobView is the wire representation of a claimed job handed to the worker.
type JobView struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	AssetID   string            `json:"assetId"`
	MediaPath string            `json:"mediaPath"`
	Params    map[string]string `json:"params"`
	Status    string            `json:"status"`
}

// ClaimResponse is the body returned by POST /claim.
type ClaimResponse struct {
	Jobs []JobView `json:"jobs"`
}

// FailRequest is the body of POST /{id}/fail.
type FailRequest struct {
	Reason string `json:"reason"`
}

func jobView(j catalog.Job) JobView {
	return JobView{
		ID:        j.ID,
		Kind:      j.Kind,
		AssetID:   j.AssetID,
		MediaPath: string(j.MediaPath),
		Params:    j.Params,
		Status:    j.Status,
	}
}

func (a *API) handleClaim(w http.ResponseWriter, r *http.Request) {
	var req ClaimRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.N <= 0 {
		req.N = 1
	}
	lease := time.Duration(req.LeaseSeconds) * time.Second
	if lease <= 0 {
		lease = 60 * time.Second
	}
	jobs, err := a.queue.Claim(r.Context(), req.Kinds, lease, req.N)
	if err != nil {
		a.log.Error("jobs api: claim", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "claim failed")
		return
	}
	views := make([]JobView, 0, len(jobs))
	for _, j := range jobs {
		views = append(views, jobView(j))
	}
	httpx.WriteJSON(w, http.StatusOK, ClaimResponse{Jobs: views})
}

// handleSource streams the job's source file to the worker. The path is the
// job's stored MediaPath, resolved and opened through the traversal-proof
// resolver — the worker never supplies a filesystem path, so a job row can only
// ever name a file inside a configured library root.
func (a *API) handleSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := a.queue.Get(r.Context(), id)
	if err != nil {
		a.writeJobError(w, err)
		return
	}
	f, err := a.resolver.Open(mediapath.MediaPath(job.MediaPath))
	if err != nil {
		// Includes ErrEscapesRoot / unknown alias — mapped to 4xx by httpx.
		a.log.Warn("jobs api: open source", "job", id, "path", job.MediaPath, "err", err)
		httpx.WriteAPIError(w, err)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "stat source failed")
		return
	}
	if info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_path", "source is a directory")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	// http.ServeContent handles Range requests, Content-Length and modtime for
	// large source files so the worker can fetch efficiently.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// handleResult ingests the derivative/result a worker produced for a job. It
// accepts either multipart/form-data (a "file" part with the derivative bytes
// plus optional "kind", "mime" and a JSON "data" field) or a raw body (the
// derivative bytes, with metadata supplied via query/header). The result is
// handed to the registered handler for the job's kind, which persists the
// derivative (via the Cache) and/or structured data. This does NOT mark the
// job complete — the worker calls /complete after a successful result.
func (a *API) handleResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := a.queue.Get(r.Context(), id)
	if err != nil {
		a.writeJobError(w, err)
		return
	}

	result, err := parseResult(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.registry.Handle(r.Context(), job, result); err != nil {
		a.log.Error("jobs api: result handler", "job", id, "kind", job.Kind, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "result_failed", "storing result failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "stored"})
}

func (a *API) handleComplete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Complete carries no required body; an optional JSON result Data may be
	// posted to persist structured output recorded with the terminal row.
	var result catalog.JobResult
	if r.ContentLength > 0 {
		var body struct {
			Data map[string]any `json:"data"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		result.Data = body.Data
	}
	if err := a.queue.Complete(r.Context(), id, result); err != nil {
		a.writeJobError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (a *API) handleFail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req FailRequest
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	if req.Reason == "" {
		req.Reason = "unspecified worker failure"
	}
	if err := a.queue.Fail(r.Context(), id, req.Reason); err != nil {
		a.writeJobError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "failed"})
}

// writeJobError maps queue/job errors to the right status: a not-found/lapsed
// job is 404, everything else 500.
func (a *API) writeJobError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrJobNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "job_not_found", err.Error())
		return
	}
	a.log.Error("jobs api: job error", "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal", "internal error")
}

// drainAndClose fully drains then closes a body part so keep-alive connections
// are reusable; errors are ignored (best-effort cleanup).
func drainAndClose(rc io.ReadCloser) {
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
