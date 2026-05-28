// Package jobs implements the durable SQLite-backed job queue (catalog.Queue)
// and the internal HTTP job API the relocatable worker pulls from. The queue is
// the single source of truth for heavy work (transcode/develop-raw/convert-
// image/enrich/face-index): it survives restarts, leaves jobs pending while no
// worker is connected (browsing never blocks), and leases claims so a stalled
// worker's jobs become claimable again without ever being run twice.
//
// jobs implements catalog.Queue and never imports the producer packages
// (media-pipeline, search-index, ai-people); those depend only on
// catalog.Queue. Result handling for each kind is plugged in at serve startup
// via the Registry, so jobs need not import internal/cache or internal/people
// either — avoiding an import cycle.
package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db/store"
)

// Job status values stored in the jobs table.
const (
	statusPending   = "pending"
	statusClaimed   = "claimed"
	statusCompleted = "completed"
	statusFailed    = "failed"
)

// defaultMaxAttempts caps how many times a job is retried before it is failed
// terminally. A claim bumps the attempt counter, so the Nth claim that fails
// (with N == max) gives up rather than rescheduling.
const defaultMaxAttempts = 5

// backoff bounds for the retry schedule (exponential in the attempt count).
const (
	backoffBase = 10 * time.Second
	backoffMax  = 10 * time.Minute
)

// ErrJobNotFound is returned when an id does not match a claimed job (e.g. the
// lease already lapsed, or the job was completed/failed by someone else).
var ErrJobNotFound = errors.New("jobs: job not found or not claimed")

// Queue is the durable, SQLite-backed catalog.Queue. All access goes through
// the single-writer *sql.DB the platform opens, so claims are serialized and a
// job is never handed to two workers.
type Queue struct {
	db *sql.DB
	q  *store.Queries

	// maxAttempts is the retry ceiling stamped onto newly-enqueued jobs.
	maxAttempts int64
	// backoffBase/backoffMax bound the exponential retry delay; configurable so
	// tests can shrink them.
	backoffBase time.Duration
	backoffMax  time.Duration
}

var _ catalog.Queue = (*Queue)(nil)

// Option customises a Queue.
type Option func(*Queue)

// WithMaxAttempts overrides the retry ceiling for jobs enqueued by this queue.
func WithMaxAttempts(n int) Option {
	return func(qu *Queue) {
		if n > 0 {
			qu.maxAttempts = int64(n)
		}
	}
}

// WithBackoff overrides the retry backoff base and cap. Both must be positive;
// it is used by tests to keep retries near-immediate.
func WithBackoff(base, max time.Duration) Option {
	return func(qu *Queue) {
		if base > 0 {
			qu.backoffBase = base
		}
		if max > 0 {
			qu.backoffMax = max
		}
	}
}

// NewQueue builds a Queue over the given database handle.
func NewQueue(dbh *sql.DB, opts ...Option) *Queue {
	qu := &Queue{
		db:          dbh,
		q:           store.New(dbh),
		maxAttempts: defaultMaxAttempts,
		backoffBase: backoffBase,
		backoffMax:  backoffMax,
	}
	for _, o := range opts {
		o(qu)
	}
	return qu
}

// Enqueue inserts a pending job and returns it. Params is marshalled to JSON;
// a nil map is stored as an empty object.
func (qu *Queue) Enqueue(ctx context.Context, kind, assetID string, mp catalog.MediaPath, params map[string]string) (catalog.Job, error) {
	id, err := newID()
	if err != nil {
		return catalog.Job{}, err
	}
	paramsJSON, err := marshalParams(params)
	if err != nil {
		return catalog.Job{}, err
	}
	row, err := qu.q.EnqueueJob(ctx, store.EnqueueJobParams{
		ID:          id,
		Kind:        kind,
		AssetID:     assetID,
		MediaPath:   string(mp),
		Params:      paramsJSON,
		MaxAttempts: qu.maxAttempts,
	})
	if err != nil {
		return catalog.Job{}, fmt.Errorf("jobs: enqueue: %w", err)
	}
	return toCatalogJob(row)
}

// Claim leases up to n jobs matching kinds for the given lease duration. Kinds
// is a post-filter: the SQL selects the oldest claimable rows regardless of
// kind, then this method skips and re-leases any that do not match, so callers
// asking for a narrow kind set still get fair, durable behaviour. The whole
// scan-and-claim runs in one write transaction over the single-writer
// connection, so two concurrent Claim calls can never take the same row.
func (qu *Queue) Claim(ctx context.Context, kinds []string, lease time.Duration, n int) ([]catalog.Job, error) {
	if n <= 0 {
		return nil, nil
	}
	if lease <= 0 {
		return nil, errors.New("jobs: claim lease must be positive")
	}
	want := kindSet(kinds)

	tx, err := qu.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("jobs: claim begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := qu.q.WithTx(tx)

	leaseMod := durationModifier(lease)

	// Over-select so kind-mismatched rows do not starve a small n. A claimable
	// row that does not match is skipped (left pending), not consumed.
	limit := int64(n)
	if len(want) > 0 {
		limit *= 4
	}
	ids, err := qtx.SelectClaimable(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("jobs: select claimable: %w", err)
	}

	claimed := make([]catalog.Job, 0, n)
	for _, id := range ids {
		if len(claimed) >= n {
			break
		}
		row, err := qtx.ClaimJob(ctx, store.ClaimJobParams{ID: id, Datetime: leaseMod})
		if errors.Is(err, sql.ErrNoRows) {
			// Lost the race / row changed; skip it.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("jobs: claim %s: %w", id, err)
		}
		if len(want) > 0 && !want[row.Kind] {
			// Not a kind this worker handles: release it back to pending so a
			// matching worker can take it, and do not count it.
			if _, err := qtx.RetryJob(ctx, store.RetryJobParams{
				ID:        id,
				LastError: nil,
				Datetime:  "+0 seconds",
			}); err != nil {
				return nil, fmt.Errorf("jobs: release %s: %w", id, err)
			}
			continue
		}
		job, err := toCatalogJob(row)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, job)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("jobs: claim commit: %w", err)
	}
	return claimed, nil
}

// Complete marks a claimed job completed, persisting any structured result
// Data. The derivative Bytes are stored by the API layer via the Cache before
// Complete is called; the queue only records terminal state and Data.
func (qu *Queue) Complete(ctx context.Context, id string, result catalog.JobResult) error {
	var dataJSON *string
	if len(result.Data) > 0 {
		b, err := json.Marshal(result.Data)
		if err != nil {
			return fmt.Errorf("jobs: marshal result data: %w", err)
		}
		s := string(b)
		dataJSON = &s
	}
	rows, err := qu.q.CompleteJob(ctx, store.CompleteJobParams{ID: id, ResultData: dataJSON})
	if err != nil {
		return fmt.Errorf("jobs: complete %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return nil
}

// Fail records a failed attempt. If the job still has attempts left it is
// rescheduled with exponential backoff (back to pending, available in the
// future); once attempts reach max_attempts it is failed terminally.
func (qu *Queue) Fail(ctx context.Context, id, reason string) error {
	job, err := qu.q.GetJob(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrJobNotFound, id)
		}
		return fmt.Errorf("jobs: fail get %s: %w", id, err)
	}
	if job.Status != statusClaimed {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}

	reasonPtr := &reason
	if job.Attempts >= job.MaxAttempts {
		rows, err := qu.q.FailJob(ctx, store.FailJobParams{ID: id, LastError: reasonPtr})
		if err != nil {
			return fmt.Errorf("jobs: fail %s: %w", id, err)
		}
		if rows == 0 {
			return fmt.Errorf("%w: %s", ErrJobNotFound, id)
		}
		return nil
	}

	rows, err := qu.q.RetryJob(ctx, store.RetryJobParams{
		ID:        id,
		LastError: reasonPtr,
		Datetime:  durationModifier(qu.backoffFor(job.Attempts)),
	})
	if err != nil {
		return fmt.Errorf("jobs: retry %s: %w", id, err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return nil
}

// Get returns the current persisted job by id (used by the API to resolve the
// source path for a claimed job). It is not part of catalog.Queue.
func (qu *Queue) Get(ctx context.Context, id string) (catalog.Job, error) {
	row, err := qu.q.GetJob(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.Job{}, fmt.Errorf("%w: %s", ErrJobNotFound, id)
		}
		return catalog.Job{}, fmt.Errorf("jobs: get %s: %w", id, err)
	}
	return toCatalogJob(row)
}

// CountByStatus returns how many jobs currently hold the given status; handy
// for tests and a future stats endpoint.
func (qu *Queue) CountByStatus(ctx context.Context, status string) (int, error) {
	c, err := qu.q.CountJobsByStatus(ctx, status)
	return int(c), err
}

// backoffFor returns the retry delay after a failed attempt: exponential in the
// attempt count, capped at the queue's backoffMax. attempts is the count
// *after* the claim bump, so the first failure (attempts==1) waits backoffBase.
func (qu *Queue) backoffFor(attempts int64) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := qu.backoffBase
	for i := int64(1); i < attempts; i++ {
		d *= 2
		if d >= qu.backoffMax {
			return qu.backoffMax
		}
	}
	if d > qu.backoffMax {
		d = qu.backoffMax
	}
	return d
}

// durationModifier renders a duration as a SQLite datetime modifier string,
// e.g. 30*time.Second -> "+30 seconds". Sub-second precision is truncated to
// whole seconds (the queue scheduling granularity).
func durationModifier(d time.Duration) string {
	secs := int64(d / time.Second)
	return fmt.Sprintf("+%d seconds", secs)
}

// kindSet builds a lookup set from a kinds slice (nil/empty => match any).
func kindSet(kinds []string) map[string]bool {
	if len(kinds) == 0 {
		return nil
	}
	m := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	return m
}

// newID returns a random 128-bit opaque job id as lowercase hex.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("jobs: generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// marshalParams serialises a params map to a JSON object string ("{}" if nil).
func marshalParams(params map[string]string) (string, error) {
	if len(params) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("jobs: marshal params: %w", err)
	}
	return string(b), nil
}

// toCatalogJob maps a stored row to the shared catalog.Job, decoding params.
func toCatalogJob(row store.Job) (catalog.Job, error) {
	params := map[string]string{}
	if row.Params != "" && row.Params != "{}" {
		if err := json.Unmarshal([]byte(row.Params), &params); err != nil {
			return catalog.Job{}, fmt.Errorf("jobs: decode params for %s: %w", row.ID, err)
		}
	}
	return catalog.Job{
		ID:        row.ID,
		Kind:      row.Kind,
		AssetID:   row.AssetID,
		MediaPath: catalog.MediaPath(row.MediaPath),
		Params:    params,
		Status:    row.Status,
	}, nil
}
