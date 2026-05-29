# Teammate Spec — jobs-worker

Read `docs/specs/_contracts.md` and `docs/TECH_SPEC.md` §3,7.3. **Depends on**
backend-platform + media-pipeline (catalog).

## Objective
The durable job system + the relocatable GPU transform worker. Heavy media work
(transcode, RAW develop, WebP/AVIF) runs here, off the serve host, via ffmpeg + GPU where
available. ai-people plugs its `face-index`/`enrich-ai` jobs into the same machinery.

## Owned packages
`internal/{jobs,worker,transform}`, migrations `0300–0399`, queries
`internal/db/queries/jobs.sql`.

## Component design
1. **jobs**: durable **SQLite-backed queue** implementing `Queue` (`_contracts.md §4`):
   enqueue, claim-with-lease (visibility timeout + retry/backoff), complete, fail. Survives
   restarts; offline worker ⇒ jobs wait (browsing never blocks).
2. **Internal job HTTP API** (mounted under serve, behind shared-secret from `[worker]`):
   - `POST /internal/jobs/claim` `{kinds,lease,n}` → `[Job]`
   - `GET  /internal/jobs/{id}/source` → stream source bytes (resolver-validated)
   - `POST /internal/jobs/{id}/result` (multipart/bytes + JSON `Data`) → store derivative
     via Cache and/or structured data (hand to catalog/search/people via callbacks)
   - `POST /internal/jobs/{id}/complete|fail`
   Define a registration so result handlers for each kind can be supplied by other teammates.
3. **transform** (worker side): detect ffmpeg + GPU accel (NVENC/QSV/VAAPI), CPU fallback.
   - `transcode-video`: HEVC/.mov → H.264/AAC MP4 (and/or HLS) browser-friendly.
   - `develop-raw`: RAW-only → JPG via `dcraw -e`/exiftool embedded preview (fast path) or
     `darktable-cli`/ffmpeg full develop. Output is a managed derivative (`developed-jpg`),
     **never written into the library**.
   - `convert-image`: WebP/AVIF derivative.
4. **worker** (`enrich-worker` command): long-running client — claim jobs from `--server`
   with `--secret`, fetch source, run transform (or delegate enrich/face to ai-people's
   handlers via interface), post result, complete/fail. Concurrency-limited; backoff.

## Interfaces
Implement: `jobs.Queue`, internal job API + result-handler registry, transform runners, the
`enrich-worker` command. Consume: mediapath, cache, db store, config; accept pluggable
`Enricher`/`FaceRecognizer`/transform handlers (ai-people registers theirs).

## Tests (required)
- queue: enqueue/claim/lease-expiry/retry/complete/fail; concurrency (no double-claim);
  restart durability (temp sqlite).
- internal API (httptest): shared-secret auth, claim→source→result→complete round-trip;
  source path is resolver-validated (traversal reject).
- transform: ffmpeg/dcraw invocations behind an interface with a fake for unit tests; real
  ffmpeg/dcraw runs are `//go:build integration` (transcode a small mp4, develop a CR3 from
  `~/Pictures`).

## Acceptance
A job enqueued by ingest is claimed by a worker, source fetched, derivative produced &
cached, asset updated. Worker runs against a live `serve`. `go test -race` green.

## Verified facts
Serve host has **no GPU**; worker (desktop) does — transforms must run worker-side. RAW =
Canon CR2/CR3. Real video samples in `~/Pictures` (mp4/mov/avi).
