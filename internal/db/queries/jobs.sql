-- jobs-worker queries over the durable job queue (see migration 0300_jobs.sql).
-- The claim path is split into a SELECT of claimable ids and a per-id UPDATE so
-- it can run inside one serialized write transaction; with the single-writer
-- SQLite connection this guarantees no two claims ever take the same row.

-- name: EnqueueJob :one
INSERT INTO jobs (id, kind, asset_id, media_path, params, max_attempts, status, available_at)
VALUES (?, ?, ?, ?, ?, ?, 'pending', datetime('now'))
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = ?;

-- SelectClaimable returns up to ? ids of jobs that are claimable right now: a
-- pending job whose availability has arrived, or a claimed job whose lease has
-- lapsed (worker died/stalled). Oldest-available first for FIFO fairness.
-- name: SelectClaimable :many
SELECT id FROM jobs
WHERE available_at <= datetime('now')
  AND (
        status = 'pending'
     OR (status = 'claimed' AND (lease_until IS NULL OR lease_until <= datetime('now')))
  )
ORDER BY available_at ASC, created_at ASC
LIMIT ?;

-- ClaimJob marks one selected row claimed, sets its lease and bumps attempts.
-- It is guarded by the same predicate as SelectClaimable so a row that changed
-- between select and update (impossible under the single writer, but correct
-- regardless) is not double-claimed.
-- name: ClaimJob :one
UPDATE jobs
SET status = 'claimed',
    lease_until = datetime('now', ?),
    attempts = attempts + 1,
    updated_at = datetime('now')
WHERE id = ?
  AND available_at <= datetime('now')
  AND (
        status = 'pending'
     OR (status = 'claimed' AND (lease_until IS NULL OR lease_until <= datetime('now')))
  )
RETURNING *;

-- name: CompleteJob :execrows
UPDATE jobs
SET status = 'completed',
    lease_until = NULL,
    result_data = ?,
    last_error = NULL,
    updated_at = datetime('now')
WHERE id = ? AND status = 'claimed';

-- RetryJob reschedules a failed-but-retryable job: back to pending with a
-- future availability (caller computes the backoff offset string).
-- name: RetryJob :execrows
UPDATE jobs
SET status = 'pending',
    lease_until = NULL,
    last_error = ?,
    available_at = datetime('now', ?),
    updated_at = datetime('now')
WHERE id = ? AND status = 'claimed';

-- name: FailJob :execrows
UPDATE jobs
SET status = 'failed',
    lease_until = NULL,
    last_error = ?,
    updated_at = datetime('now')
WHERE id = ? AND status = 'claimed';

-- name: CountJobsByStatus :one
SELECT COUNT(*) FROM jobs WHERE status = ?;
