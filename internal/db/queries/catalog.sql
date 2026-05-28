-- media-pipeline catalog queries (sqlc). Domain: assets / files / derivatives.
-- See docs/specs/_contracts.md sec.3 for the type shapes and sec.6 for conventions.

-- name: UpsertAsset :exec
INSERT INTO assets (id, alias, dir, base_name, kind, display_path, needs_develop, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
ON CONFLICT(id) DO UPDATE SET
    alias         = excluded.alias,
    dir           = excluded.dir,
    base_name     = excluded.base_name,
    kind          = excluded.kind,
    display_path  = excluded.display_path,
    needs_develop = excluded.needs_develop,
    updated_at    = excluded.updated_at;

-- name: UpsertFile :exec
INSERT INTO files (media_path, asset_id, kind, size, mod_time, hash)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(media_path) DO UPDATE SET
    asset_id = excluded.asset_id,
    kind     = excluded.kind,
    size     = excluded.size,
    mod_time = excluded.mod_time,
    hash     = excluded.hash;

-- name: GetAsset :one
SELECT id, alias, dir, base_name, kind, display_path, needs_develop, captured_at, created_at, updated_at
FROM assets WHERE id = ?;

-- name: ListFilesForAsset :many
SELECT media_path, asset_id, kind, size, mod_time, hash
FROM files WHERE asset_id = ?
ORDER BY media_path;

-- name: ListAssetsByDir :many
-- Keyset (cursor) pagination by base_name within a folder. The cursor is the
-- last base_name seen; pass "" to start. Fetches limit+1 so the caller can
-- detect a next page.
SELECT id, alias, dir, base_name, kind, display_path, needs_develop, captured_at, created_at, updated_at
FROM assets
WHERE alias = ? AND dir = ? AND base_name > ?
ORDER BY base_name
LIMIT ?;

-- name: ListDirsForLibrary :many
-- All distinct (non-empty) directories holding assets in a library. The browse
-- handler derives immediate child folders of a given dir from this in Go (the
-- substring arithmetic SQLite would need is brittle through sqlc's parser).
SELECT DISTINCT dir FROM assets WHERE alias = ? AND dir <> '' ORDER BY dir;

-- name: ListAllFiles :many
-- Full snapshot of catalogued files for a library, used by the reconcile scan
-- to diff against the on-disk walk on (path, mtime, size).
SELECT media_path, asset_id, kind, size, mod_time, hash
FROM files WHERE asset_id IN (SELECT id FROM assets WHERE alias = ?)
ORDER BY media_path;

-- name: GetFile :one
SELECT media_path, asset_id, kind, size, mod_time, hash
FROM files WHERE media_path = ?;

-- name: DeleteFile :exec
DELETE FROM files WHERE media_path = ?;

-- name: CountFilesForAsset :one
SELECT count(*) FROM files WHERE asset_id = ?;

-- name: DeleteAsset :exec
DELETE FROM assets WHERE id = ?;

-- name: ListAllAssets :many
-- All assets in a library (for warm-cache / bulk passes), folder order.
SELECT id, alias, dir, base_name, kind, display_path, needs_develop, captured_at, created_at, updated_at
FROM assets WHERE alias = ?
ORDER BY dir, base_name;

-- name: SetAssetCapturedAt :exec
UPDATE assets SET captured_at = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpsertDerivative :exec
INSERT INTO derivatives (asset_id, kind, params, path, mime, src_hash)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(asset_id, kind, params) DO UPDATE SET
    path     = excluded.path,
    mime     = excluded.mime,
    src_hash = excluded.src_hash,
    created_at = datetime('now');

-- name: GetDerivative :one
SELECT asset_id, kind, params, path, mime, src_hash, created_at
FROM derivatives WHERE asset_id = ? AND kind = ? AND params = ?;

-- name: ListDerivativesForAsset :many
SELECT asset_id, kind, params, path, mime, src_hash, created_at
FROM derivatives WHERE asset_id = ?;

-- name: DeleteDerivativesForAsset :exec
DELETE FROM derivatives WHERE asset_id = ?;
