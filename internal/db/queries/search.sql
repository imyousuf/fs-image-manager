-- search-index queries (sqlc). Domains: metadata (normalized EXIF/ffprobe
-- facts), the assets_fts full-text index, and the semantic-search embeddings.
-- See migration 0200_metadata_search.sql for the schema and
-- docs/specs/_contracts.md sec.6 for conventions. KEEP THIS FILE PURE ASCII:
-- sqlc v1.31 corrupts generated constants on any non-ASCII byte.

-- name: UpsertMetadata :exec
INSERT INTO metadata (
    asset_id, captured_at, camera_make, camera_model, lens,
    width, height, orientation, duration_ms, codec, gps_lat, gps_lng, source,
    indexed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(asset_id) DO UPDATE SET
    captured_at  = excluded.captured_at,
    camera_make  = excluded.camera_make,
    camera_model = excluded.camera_model,
    lens         = excluded.lens,
    width        = excluded.width,
    height       = excluded.height,
    orientation  = excluded.orientation,
    duration_ms  = excluded.duration_ms,
    codec        = excluded.codec,
    gps_lat      = excluded.gps_lat,
    gps_lng      = excluded.gps_lng,
    source       = excluded.source,
    indexed_at   = datetime('now');

-- name: GetMetadata :one
SELECT asset_id, captured_at, camera_make, camera_model, lens,
       width, height, orientation, duration_ms, codec, gps_lat, gps_lng,
       source, indexed_at
FROM metadata WHERE asset_id = ?;

-- name: DeleteMetadata :exec
DELETE FROM metadata WHERE asset_id = ?;

-- name: ListCameras :many
-- Distinct non-empty camera models in a library, for the camera facet listing.
SELECT DISTINCT m.camera_model
FROM metadata m
JOIN assets a ON a.id = m.asset_id
WHERE a.alias = ? AND m.camera_model <> ''
ORDER BY m.camera_model;

-- name: UpsertEmbedding :exec
INSERT INTO embeddings (asset_id, dim, vec, updated_at)
VALUES (?, ?, ?, datetime('now'))
ON CONFLICT(asset_id) DO UPDATE SET
    dim        = excluded.dim,
    vec        = excluded.vec,
    updated_at = datetime('now');

-- name: GetEmbedding :one
SELECT asset_id, dim, vec FROM embeddings WHERE asset_id = ?;

-- name: ListEmbeddings :many
-- All embeddings (asset_id + vector) for the brute-force cosine search. At
-- personal-library scale this full scan is cheap; if it ever isn't, this is the
-- single query to replace with an ANN index.
SELECT asset_id, dim, vec FROM embeddings;

-- name: DeleteEmbedding :exec
DELETE FROM embeddings WHERE asset_id = ?;
