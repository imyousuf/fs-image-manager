-- Platform-owned queries over the app_meta key/value table.

-- name: GetMeta :one
SELECT value FROM app_meta WHERE key = ?;

-- name: SetMeta :exec
INSERT INTO app_meta (key, value, updated_at)
VALUES (?, ?, datetime('now'))
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;
