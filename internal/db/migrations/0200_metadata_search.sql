-- +goose Up
-- search-index owned schema (range 0200-0299; see docs/specs/_contracts.md sec.6).
-- Three concerns: (1) a normalized metadata table keyed by asset id, holding the
-- EXIF/ffprobe facts we search and facet over; (2) an FTS5 virtual table for
-- full-text relevance ranking over the human-readable fields; (3) an embeddings
-- table that ai-people fills later for semantic (vector) search. Browsing never
-- depends on any of these: they are filled during ingest as a side effect and a
-- missing row just means "not indexed yet".

-- metadata is one row per asset (1:1 with assets.id; cascade-deleted with it).
-- Columns are the normalized, queryable facts: capture time drives the timeline
-- and date facet; camera/lens drive the camera facet and search; width/height/
-- orientation describe the image; duration/codec describe video; gps is sparse
-- (DSLRs rarely geotag) so the lat/lng pair is nullable and secondary.
CREATE TABLE metadata (
    asset_id     TEXT PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    captured_at  TEXT,                      -- RFC3339 capture time (NULL if unknown)
    camera_make  TEXT NOT NULL DEFAULT '',  -- e.g. "Canon"
    camera_model TEXT NOT NULL DEFAULT '',  -- e.g. "Canon EOS R5"
    lens         TEXT NOT NULL DEFAULT '',  -- lens model string, when present
    width        INTEGER NOT NULL DEFAULT 0,
    height       INTEGER NOT NULL DEFAULT 0,
    orientation  INTEGER NOT NULL DEFAULT 0,-- EXIF orientation (1..8, 0 = unknown)
    duration_ms  INTEGER NOT NULL DEFAULT 0,-- video duration in milliseconds
    codec        TEXT NOT NULL DEFAULT '',  -- video codec (e.g. "h264", "hevc")
    gps_lat      REAL,                      -- nullable; sparse on DSLR libraries
    gps_lng      REAL,
    source       TEXT NOT NULL DEFAULT '',  -- "exif" | "ffprobe" | "" (none)
    indexed_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- The timeline groups by date(captured_at); this index makes that bucketing and
-- the date-range search filter an ordered scan rather than a full table read.
CREATE INDEX idx_metadata_captured ON metadata (captured_at);

-- The camera facet filters by exact model; an index keeps it cheap at scale.
CREATE INDEX idx_metadata_camera ON metadata (camera_model);

-- assets_fts is the full-text relevance index. It is a content-less FTS5 table
-- (no external content table) keyed by the asset id stored in a non-indexed
-- column, so we can map a MATCH hit straight back to an asset. The columns are
-- the human-readable, searchable fields. name/camera/lens are filled by
-- search-index now; labels/caption/ocr/persons are filled by ai-people later and
-- stay empty strings until then -- FTS5 columns are always TEXT and tolerate
-- empty values, so designing them now costs nothing.
-- +goose StatementBegin
CREATE VIRTUAL TABLE assets_fts USING fts5 (
    asset_id UNINDEXED,
    name,
    camera,
    lens,
    labels,
    caption,
    ocr,
    persons
);
-- +goose StatementEnd

-- embeddings stores one semantic-search vector per asset. ai-people computes the
-- vector on the worker and calls the embedding store's UpsertEmbedding; search
-- does brute-force cosine over this table (pure Go, no C extension / vector ext),
-- which is fine at personal-library scale. The vector is a little-endian packed
-- []float32 blob; dim records its length so a malformed/short blob is rejected.
CREATE TABLE embeddings (
    asset_id   TEXT PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
    dim        INTEGER NOT NULL,
    vec        BLOB NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- +goose Down
DROP TABLE embeddings;
DROP TABLE assets_fts;
DROP INDEX idx_metadata_camera;
DROP INDEX idx_metadata_captured;
DROP TABLE metadata;
