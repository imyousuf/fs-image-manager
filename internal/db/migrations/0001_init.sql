-- +goose Up
-- Platform-owned foundation schema. Domain tables (catalog, metadata, FTS5,
-- job queue, faces, ...) are added by their owning teammates in their reserved
-- migration ranges; see docs/specs/_contracts.md §6.

-- app_meta is a small key/value store for platform-level bookkeeping such as
-- schema/app version and last-scan markers.
CREATE TABLE app_meta (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO app_meta (key, value) VALUES ('schema_version', '1');

-- +goose Down
DROP TABLE app_meta;
