-- +goose Up
-- media-pipeline catalog schema (migration range 0100-0199; see
-- docs/specs/_contracts.md §6). The catalog is organised around *assets* — one
-- logical photo or video — each bundling one or more files on disk (e.g. a RAW
-- capture, its JPG sibling and an XMP sidecar). Derivatives (thumbnails,
-- posters, previews, transcodes, …) are cached transform outputs keyed by
-- source content hash; they live in the managed cache dir, never the library.

-- assets is the logical media unit. Its id is the stable hash of
-- "<alias>/<dir>/<basename-without-ext>" so re-scanning the same logical photo
-- keeps the same id even as member files come and go. captured_at is filled by
-- search-index from metadata; it stays NULL until then.
CREATE TABLE assets (
    id          TEXT PRIMARY KEY,
    alias       TEXT NOT NULL,
    dir         TEXT NOT NULL,          -- library-relative containing dir ("" = root)
    base_name   TEXT NOT NULL,          -- filename without extension
    kind        TEXT NOT NULL,          -- "image" | "video"
    display_path TEXT NOT NULL,         -- MediaPath used for thumbs/preview ("" = needs develop)
    needs_develop INTEGER NOT NULL DEFAULT 0, -- 1 = RAW-only, awaiting a develop-raw job
    captured_at TEXT,                   -- RFC3339, set by search-index; NULL until enriched
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Browse queries always filter by (alias, dir) and order by base_name; this
-- composite index serves both the listing and the keyset pagination cursor.
CREATE INDEX idx_assets_alias_dir ON assets (alias, dir, base_name);

-- files are the on-disk members of an asset. media_path is the alias-prefixed
-- path and is globally unique (one row per physical file). Deleting an asset
-- cascades to its files.
CREATE TABLE files (
    media_path TEXT PRIMARY KEY,        -- "<alias>/<relpath>"
    asset_id   TEXT NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,           -- jpg|png|raw|video|sidecar|other
    size       INTEGER NOT NULL,
    mod_time   TEXT NOT NULL,           -- RFC3339Nano
    hash       TEXT NOT NULL DEFAULT '' -- content hash (head+size); see internal/media
);

CREATE INDEX idx_files_asset ON files (asset_id);

-- derivatives records cached transform outputs so warm-cache / serving can
-- skip regeneration. The on-disk bytes live under the cache dir at path; this
-- table is the index. A given (asset, kind, params) is unique. Deleting an
-- asset cascades, letting prune drop the rows (the cache files are content
-- addressed and reaped separately).
CREATE TABLE derivatives (
    asset_id TEXT NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,             -- thumb|poster|preview|webp|mp4|developed-jpg
    params   TEXT NOT NULL DEFAULT '',  -- e.g. "w=320"
    path     TEXT NOT NULL,             -- absolute path in the cache dir
    mime     TEXT NOT NULL,
    src_hash TEXT NOT NULL DEFAULT '',  -- source content hash this was derived from
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (asset_id, kind, params)
);

-- +goose Down
DROP TABLE derivatives;
DROP TABLE files;
DROP TABLE assets;
