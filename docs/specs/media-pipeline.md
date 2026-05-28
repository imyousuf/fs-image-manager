# Teammate Spec — media-pipeline

Read `docs/specs/_contracts.md` and `docs/TECH_SPEC.md` §6,7. **Depends on** backend-platform
(mediapath, db, server hook, fixtures).

## Objective
The media model + the fast browsing experience: catalog (assets/files/derivatives),
RAW+JPG+sidecar grouping, thumbnail/poster cache, video range streaming, and the hands-off
ingestion pipeline (watcher + 5-min debounce + reconcile scan).

## Owned packages
`internal/{catalog,media,cache,ingest}`, migrations `0100–0199`, queries
`internal/db/queries/catalog.sql`.

## Component design
1. **catalog**: own the shared types in `_contracts.md §3`. Repo over sqlc store: upsert
   assets/files, list by `(alias,dir)`, get by ID. **Asset grouping**: files sharing
   `(alias,dir,basename-sans-ext)` collapse into one Asset. Determine `DisplayPath`:
   prefer `.jpg/.jpeg`, else other image, else (RAW-only) mark "needs develop" (enqueue
   `develop-raw` via jobs Queue when available; until then poster=placeholder). Sidecars
   (`.xmp/.pp3/.thm`) attach as `Kind=sidecar`, never standalone assets. `Kind=video` for
   `.mp4/.mov/.mkv/.webm/.avi`.
2. **media**: decode images (`disintegration/imaging`), generate thumbnails/previews; video
   **poster** via single-frame ffmpeg (optional — placeholder if ffmpeg absent); content
   hash helper. Image ops are CPU/host; never decode RAW here (that's the worker).
3. **cache**: implement `Cache` (`_contracts.md §4`) — content-addressed dir
   (`<cache>/<ab>/<hash>-<kind>-<params>`), ETag/Cache-Control on serve.
4. **ingest**: `fsnotify` watch of all library roots + **trailing debounce** (default 300s,
   from config; resets on each event) + **periodic reconcile scan** (walk vs catalog on
   path+mtime+size). Per-file **stability check** (mtime+size stable across window) before
   processing. Adds/mods → upsert + enqueue thumb/poster (+ develop-raw / enrich) jobs;
   deletes → prune catalog + derivatives. Fully automatic.
5. **HTTP handlers** (register via platform hook): `/api/libraries`, `/api/assets` (browse),
   `/api/assets/{id}/thumb|preview|stream|download`, `/api/download`, `/api/upload`. `stream`
   uses `http.ServeContent` for Range. `download` zips validated paths. `upload` writes via
   resolver then triggers ingest.
6. **CLI**: implement `scan` (one-shot reconcile) and `warm-cache` (generate all
   thumbs/posters, idempotent by hash; enqueue develop/enrich) — wire into platform dispatch.

## Interfaces
Implement: `Cache`, catalog repo, ingest service, the media HTTP handlers. Consume:
`mediapath.Resolver`, db store, `jobs.Queue` (optional — guard nil so browsing works with no
worker), config.

## Tests (required)
- **RAW+JPG grouping**: CR3+JPG+XMP → one asset, DisplayPath=JPG, sidecar attached
  (regression test, per `_contracts`). RAW-only → needs-develop. `.thm` sidecar.
- thumbnail/poster **golden-file** tests; cache hit/miss + ETag.
- ingest: debounce (use a short window) waits for quiet; stability check skips a
  growing file; reconcile detects add/mod/delete; multi-root.
- handlers (httptest): browse pagination, Range on stream, download zip, traversal reject,
  upload→appears in listing.

## Acceptance
Against a 2-root temp config seeded from `~/Pictures`+`~/Videos`: browse groups assets
correctly, thumbnails cache & serve, video streams with Range, `scan`/`warm-cache` populate,
debounce holds then fires. `go test -race` green.

## Verified facts
Canon CR2/CR3 + JPG dominate; sidecars `.xmp/.pp3/.thm`; videos live in `~/Pictures`;
`~/Videos` is mostly webm.
