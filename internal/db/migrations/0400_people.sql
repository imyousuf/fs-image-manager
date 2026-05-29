-- +goose Up
-- ai-people owned schema (range 0400-0499; see docs/specs/_contracts.md section 6).
-- Two concerns: (1) people recognition -- a Person is a named cluster of Faces,
-- a Face is one detected face within an asset carrying its bounding box and an
-- external reference (a Rekognition FaceId or, later, a local vector ref); and
-- (2) an enrichment ledger so generic AI enrichment (Ollama caption/tags/embed)
-- and face indexing are cached by content hash and never recomputed for media
-- that has not changed. Browsing never depends on any of these: a library with
-- nothing recognised still browses, streams and folder-lists; the People view
-- and "photos of X" search simply return nothing until a face-index job runs.

-- persons is one row per named (or pending-unknown) cluster of faces. A cluster
-- starts life unnamed -- name = '' -- holding faces that match each other but
-- have no human label yet ("unknown" in the UI); the user later renames it. The
-- cover_face_id points at a representative face for the thumbnail. ext_collection
-- records which face-recognition backend/collection the cluster belongs to, so a
-- Rekognition-backed person and a local-vector person never get conflated.
CREATE TABLE persons (
    id             TEXT PRIMARY KEY,            -- stable opaque id
    name           TEXT NOT NULL DEFAULT '',    -- '' = unknown/pending label
    cover_face_id  TEXT NOT NULL DEFAULT '',    -- representative face for the cover thumb
    ext_collection TEXT NOT NULL DEFAULT '',    -- backend collection this cluster lives in
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

-- A person's display name is unique when set (two clusters never share a name);
-- empty names are exempt so any number of unknown clusters can coexist. SQLite
-- treats NULLs as distinct in a UNIQUE index, so the partial index keys only the
-- named rows.
CREATE UNIQUE INDEX idx_persons_name ON persons (name) WHERE name <> '';

-- faces is one row per detected face. asset_id ties it to the logical asset it
-- was found in (cascade-deleted with the asset). person_id is the cluster it was
-- assigned to (NULL while pending). bbox_* are the normalized face rectangle
-- (0..1 of image width/height) for cropping the cover/preview. ext_ref is the
-- backend's handle for the face vector: a Rekognition FaceId (indexed into a
-- Face Collection) or a local vector reference. content_hash records the hash of
-- the asset's source bytes this face was detected from, so re-running face-index
-- over unchanged media is a cache hit, not a re-detect + re-upload.
CREATE TABLE faces (
    id           TEXT PRIMARY KEY,             -- stable opaque id
    asset_id     TEXT NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    person_id    TEXT REFERENCES persons (id) ON DELETE SET NULL, -- NULL = unassigned
    bbox_x       REAL NOT NULL DEFAULT 0,       -- normalized left
    bbox_y       REAL NOT NULL DEFAULT 0,       -- normalized top
    bbox_w       REAL NOT NULL DEFAULT 0,       -- normalized width
    bbox_h       REAL NOT NULL DEFAULT 0,       -- normalized height
    confidence   REAL NOT NULL DEFAULT 0,       -- detector/match confidence (0..100)
    ext_ref      TEXT NOT NULL DEFAULT '',      -- Rekognition FaceId or local vector ref
    content_hash TEXT NOT NULL DEFAULT '',      -- source content hash this face came from
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

-- "All photos of X" walks faces by person; this index keeps that an ordered scan.
CREATE INDEX idx_faces_person ON faces (person_id);
-- The per-asset face listing (and cascade) filters by asset.
CREATE INDEX idx_faces_asset ON faces (asset_id);
-- The face-index cache check ("have we already indexed this asset's bytes?")
-- looks faces up by the asset's content hash.
CREATE INDEX idx_faces_content ON faces (content_hash);

-- enrichment_runs is the cache ledger for completed enrichment/face-index work,
-- keyed by (asset, kind, content_hash). A row's presence means "this exact kind
-- of enrichment already ran over these exact bytes" -- so an enqueue path can
-- skip an asset whose content hash already has a row, and re-running over
-- unchanged media costs nothing. kind is 'enrich-ai' or 'face-index'.
CREATE TABLE enrichment_runs (
    asset_id     TEXT NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,                 -- 'enrich-ai' | 'face-index'
    content_hash TEXT NOT NULL,
    ran_at       TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (asset_id, kind, content_hash)
);

-- +goose Down
DROP TABLE enrichment_runs;
DROP INDEX idx_faces_content;
DROP INDEX idx_faces_asset;
DROP INDEX idx_faces_person;
DROP TABLE faces;
DROP INDEX idx_persons_name;
DROP TABLE persons;
