-- +goose Up
-- jobs-worker owned schema (range 0300-0399; see docs/specs/_contracts.md §6).
-- The durable job queue backing internal/jobs' catalog.Queue. The table is the
-- single source of truth: it survives restarts, an offline worker just leaves
-- rows pending, and lease/visibility-timeout columns make claims atomic so no
-- two workers run the same job. Heavy media work (transcode/develop-raw/convert
-- /enrich/face-index) is represented as one row per job.

CREATE TABLE jobs (
    id           TEXT PRIMARY KEY,          -- random opaque id
    kind         TEXT NOT NULL,             -- catalog JobKind* constant
    asset_id     TEXT NOT NULL,             -- owning asset (may be "")
    media_path   TEXT NOT NULL,             -- alias-prefixed source path
    params       TEXT NOT NULL DEFAULT '{}',-- JSON object of string params
    status       TEXT NOT NULL DEFAULT 'pending',
                                            -- pending | claimed | completed | failed

    attempts     INTEGER NOT NULL DEFAULT 0,-- delivery attempts so far
    max_attempts INTEGER NOT NULL DEFAULT 5,-- retry ceiling before terminal fail

    -- available_at is the earliest time the job may be claimed; backoff pushes
    -- it into the future after a failure. lease_until is set while a worker
    -- holds the job; a lapsed lease makes a claimed job claimable again.
    available_at TEXT NOT NULL DEFAULT (datetime('now')),
    lease_until  TEXT,

    last_error   TEXT,                       -- most recent failure reason
    result_data  TEXT,                       -- JSON result Data from completion

    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- The claim hot path scans pending/lease-expired rows by availability; this
-- index keeps that ordered scan cheap as the queue grows.
CREATE INDEX idx_jobs_claim ON jobs (status, available_at);

-- +goose Down
DROP INDEX idx_jobs_claim;
DROP TABLE jobs;
