# fs-image-manager — Modernization Tech Spec

**Status:** Draft · **Date:** 2026-05-28 · Supersedes `ROADMAP.md`

This document is the design of record for "super-charging" fs-image-manager from a
2014–2020-era image browser into a modern, self-hosted **media** (photo + video)
manager. Each implementation milestone should be validated against this spec.

---

## 1. Context & current state

A self-hosted manager for a personal media library on a server's filesystem. A Go
backend exposes a JSON API (browse folders, scaled thumbnails/previews, multi-select
zip download, per-device download history in SQLite). The frontend was being rewritten
(branch `frontend-rewrite`) from Aurelia to React but is effectively an empty
Create-React-App shell that calls nothing.

**What we keep:** the Go backend (modernized in place).
**What we rebuild:** the frontend (fresh, since there is nothing to preserve).
**What we add:** single-binary packaging, a media model with video + RAW, a
cache/index/enrichment pipeline, a GPU transform worker, and systemd + self-update
deployment.

Known defects to fix early: arbitrary-file-read path traversal in `list-path`,
`image-scale`, `download`; broken `IsDeviceCookieStillValid`; a working-tree CI
regression (`webhook-broker-*` artifact glob); dead Travis/docker-helper remnants.

---

## 2. Goals / Non-goals

**Goals**
- Single self-contained Go binary; frontend embedded via `go:embed`.
- Treat image **and** video as first-class media; RAW-aware.
- Fast browsing of a large, large-file DSLR library via a disk-backed cache.
- Hands-off ingestion: drop files in, the system catches up automatically after a quiet period.
- Search & timeline over EXIF/video metadata; optional AI tagging/semantic search.
- Heavy/GPU work isolated in a separate, relocatable worker process.
- systemd-managed, with an in-place `update` command backed by GitHub Releases.

**Non-goals (for now)**
- Multi-user / accounts / social posting (roadmap v0.4 — minimal single-user gate only).
- Managing or replacing the existing GCS sync (treated as external offsite backup).
- Serving from GCS as primary origin.
- Cloud-only operation; everything must work fully local.

---

## 3. Architecture overview

Two run modes of the **same binary**:

```
┌─ MEDIA SERVER (CPU only; library on local disk) ─────────────────┐
│  fs-image-manager serve                                           │
│   • HTTP API + embedded Vite UI                                   │
│   • thumbnail/poster cache (cheap CPU image ops)                  │
│   • watcher (fsnotify) + 5-min debounce + reconcile scan          │
│   • SQLite (single writer): catalog, metadata, FTS5, job queue    │
│   • HTTP job API (claim / complete / fetch-source / put-result)   │
│   • range-request streaming of originals & derivatives            │
└────────────────────────────────────────────────────────────────────┘
                 ▲ pull jobs · fetch source bytes · post results (HTTPS)
                 │
┌─ DESKTOP / GPU BOX (relocatable) ────────────────────────────────┐
│  fs-image-manager enrich-worker --server=https://media-server …   │
│   • heavy transforms via ffmpeg (NVENC/QSV/VAAPI where available) │
│       - video transcode (HEVC/.mov → H.264/AAC MP4 or HLS)        │
│       - RAW-only → JPG develop (libraw/dcraw/darktable/ffmpeg)    │
│       - high-quality image conversion (WebP/AVIF)                 │
│   • AI enrichment: Ollama (VLM caption/tags + embeddings),        │
│     AWS Rekognition (labels/faces/OCR) — optional, opt-in         │
└────────────────────────────────────────────────────────────────────┘
```

Rules:
- **No shared filesystem or SQLite file across machines.** The worker reaches the
  library only through the job API (fetch source, put result).
- **serve never does GPU work and never decodes RAW.** Anything heavy is a job.
- The worker is **optional**: with no worker, browsing/thumbnails/streaming of
  natively-supported media still work; transcode/RAW-develop/AI jobs simply queue.

### Deployment topology (resolved)
- `serve` runs on the **media server, colocated with media on local disk**, no GPU.
- The media server already runs an **external script syncing media to GCS** (offsite
  backup; out of scope here).
- The **desktop is the GPU box** and runs `enrich-worker`.

### Content profile (drives priorities)
- Mostly **DSLR photos + DSLR video**; very few phone shots.
- Files are **large** → caching is load-bearing, not optional.
- **RAW+JPG** for most/all shots (see §6).
- **GPS is sparse** (DSLRs rarely geotag) → prioritize **date + camera/lens** search and
  a **timeline** over a map view (map is a minor extra for the phone subset).

---

## 4. Single binary & build

- Frontend builds with **Vite** to a known dir embedded via `//go:embed`; served with
  `http.FileServerFS`. Drops `static_file_dir` config and the `dist/web` copy step.
- **Dev:** Vite dev server proxies `/api` to a locally running `serve`; only production
  builds embed. No Go rebuild on UI change.
- Pure-Go, **CGO-free** binary (static, cross-compilable). External *runtime* deps
  (ffmpeg, Ollama, exiftool) are detected at startup and degrade gracefully if absent.

---

## 5. Backend stack decisions

| Area | From | To | Rationale |
|---|---|---|---|
| Go | 1.14 | current | toolchain already 1.26; drop `io/ioutil` |
| Routing | `gorilla/mux` (archived) | stdlib `net/http` 1.22+ ServeMux | methods + path params natively; fewer deps |
| DB driver | `mattn/go-sqlite3` (CGO) | **`modernc.org/sqlite`** (pure Go) | static single binary; FTS5 supported |
| Data access | `jinzhu/gorm` v1 (2016) | **`sqlc`** (preferred) or `gorm.io/gorm` v2 | type-checked SQL; lean. GORM v2 if ORM velocity preferred |
| DB engines | SQLite + MySQL | **SQLite only** | prod is SQLite; drop MySQL/dev complexity |
| Config | go-ini | keep (or env+flags) | minor; not a priority |
| Wiring | package-level singletons + `sync.Once` | small server/worker struct (DI) | testable handlers |

---

## 6. Media model

The catalog is organized around **assets**, not raw files.

- **Asset** = one logical photo or video, identified by directory + basename.
- An asset has one or more **files** (e.g. `IMG_1234.CR3` + `IMG_1234.JPG` → one asset,
  two files). Each file: path, kind (jpg/raw/video/…), size, mtime, content hash.
- An asset has a **display source** used for thumbnails/previews:
  - **JPG sibling present** → use the JPG; **never decode the RAW** (RAW stays cataloged
    and downloadable only).
  - **RAW-only** → enqueue a worker job to **develop a JPG derivative** (embedded-preview
    fast path, full develop for quality), stored as a managed derivative (not written
    into the user's library by default).
  - **Video** → poster frame (single-frame ffmpeg grab on the host; cheap).
- **Derivatives** (thumbnails, posters, developed JPGs, transcodes, WebP/AVIF) live in a
  managed cache keyed by `source content hash + kind + params`. Never pollute the library.
- UI shows **one item per asset**, with download options: JPG / RAW / both / original video
  / transcoded.

**Supported inputs:** images `.jpg/.jpeg/.png` (+ `.heic` minor); RAW
`.cr2/.cr3/.nef/.arw/.raf/.orf/.dng`; video `.mp4/.mov/.mkv/.webm/.avi`.

---

## 7. Caching & processing pipelines

### 7.1 Thumbnail / poster cache (host, CPU)
- Disk-backed, content-addressed (`hash+size`), served with `ETag`/`Cache-Control`.
- Image thumbnails via `disintegration/imaging` (replaces unmaintained `nfnt/resize`).
- Video posters via single-frame ffmpeg grab (skipped gracefully if ffmpeg absent).
- Decoupled from request time — requests read cache; misses enqueue/generate then cache.

### 7.2 Ingestion: watcher + debounce + reconcile (host)
- **`fsnotify`** watch (best-effort; non-recursive, unreliable on network FS) feeds a
  **trailing debounce**: each event resets a timer; work starts only after a
  **configurable quiet period (default 5 min)** of no activity. Never act mid-copy.
- **Per-file stability check** (mtime+size unchanged across the window) guards against
  partially-written files; unstable files defer to the next pass.
- **Periodic reconciliation scan** walks the tree and diffs against the catalog
  (path+mtime+size) — the source of truth and safety net for missed events.
- Detected adds/mods → enqueue thumbnail/poster + metadata + (optional) enrichment jobs.
  Deletes → prune derivatives + catalog rows.
- Fully automatic: **no command needed** for the normal "copy files over" flow.
  `warm-cache`/`scan` exist for explicit/bulk runs.

### 7.3 Heavy jobs: queue + worker
- **Durable SQLite-backed job queue** on the host (survives restarts; offline worker just
  means jobs wait — browsing never blocks).
- Job kinds: `transcode-video`, `develop-raw`, `convert-image` (WebP/AVIF), `enrich-ai`.
- **HTTP job API** (authenticated, internal):
  - `POST /internal/jobs/claim` → lease N jobs (visibility timeout, ret/backoff on fail)
  - `GET  /internal/jobs/{id}/source` → stream source bytes to the worker
  - `POST /internal/jobs/{id}/result` → upload derivative / structured result
  - `POST /internal/jobs/{id}/complete|fail`
- Worker uses **ffmpeg with GPU accel** (NVENC/QSV/VAAPI) where present, CPU fallback.

---

## 8. Search & metadata

- **Extraction:** EXIF for photos (`rwcarlsen/goexif` or `dsoprea/go-exif`), `ffprobe` for
  video (duration, codec, resolution, creation time, GPS when present).
- **Index:** normalized columns (capture date, camera make/model, lens, dimensions,
  duration) + **SQLite FTS5** virtual table over filename, tags, caption, OCR text.
- **Views enabled:** full-text + faceted search (camera/lens/date), **timeline** by
  capture date, optional map for the geotagged subset.
- **Semantic search (optional):** store embedding vectors (from the worker via Ollama);
  vector search via **`chromem-go`** (pure-Go, embedded) or brute-force cosine — fine at
  personal-library scale, keeps the single-binary ethos (no C extension).

---

## 9. Enrichment (optional, pluggable)

Two **separate** concerns — do not conflate them.

### 9.1 Generic enrichment (tags / caption / OCR / embeddings)
```
type Enricher interface {
    Enrich(ctx, asset, mediaReader) (EnrichResult, error) // labels, caption, ocrText, embedding
}
```
- **NoopEnricher** (default; EXIF/ffprobe only).
- **OllamaEnricher** — local VLM caption/tags + embeddings (semantic search §8); private,
  zero marginal cost; needs the GPU box; Ollama is its own HTTP server (point at its URL).
- **RekognitionEnricher (labels/OCR)** — cloud, opt-in.
- Local-first default. Runs as `enrich-ai` jobs on the worker; results feed the index.
  Cached by content hash — never re-compute for unchanged media.

### 9.2 People recognition (dedicated subsystem — high quality, not a gimmick)
This is the **primary motivation for Rekognition**. Generic VLMs (Ollama) describe images
but do **not** do reliable face re-identification, so this is its own pipeline:

1. **Detect** faces per image (bounding boxes).
2. **Embed** each face into a vector.
3. **Match/cluster** against a face collection → assign to a **Person**.
4. **Label**: user names a cluster; new faces auto-assign to known persons.
5. **Browse/search** "all photos of X"; a People view.

```
type FaceRecognizer interface {
    DetectAndEmbed(ctx, mediaReader) ([]Face, error)        // bbox + embedding/extId
    Match(ctx, face) (PersonID, confidence, error)          // against the collection
}
```
- **RekognitionRecognizer (default)** — AWS **Face Collections** (`IndexFaces`,
  `SearchFacesByImage`); managed, high quality, low build effort. Trade-off: uploads faces
  of family/friends to AWS — **accepted for quality** per user. No GPU needed (still routed
  through the worker/job queue for uniformity and batching/rate-limiting).
- **LocalRecognizer (alternative)** — **InsightFace/ArcFace** (detection: RetinaFace/SCRFD;
  embedding: ArcFace) on the GPU worker; high quality, fully private; more to build
  (alignment + clustering, e.g. HDBSCAN/Chinese-Whispers for unlabeled faces).
- **Catalog entities:** `Person(id, name, cover_face_id)` and
  `Face(id, asset_id, bbox, embedding|ext_face_id, person_id?, confidence)`. Unmatched
  faces land in "unknown" pending labeling. Cache by face/content hash; only index new faces.

Cost note: per-image face indexing over a large DSLR library is bounded by caching
(only new/changed assets) — never re-index unchanged media.

---

## 10. HTTP API surface (serve)

Public (UI):
- `GET /api/assets?path=…` — list a folder's assets (one entry per logical asset)
- `GET /api/assets/search?q=…&from=…&to=…&camera=…` — search/facets/timeline
- `GET /api/assets/{id}/thumb?size=…` — cached thumbnail/poster
- `GET /api/assets/{id}/preview` — display-size derivative
- `GET /api/assets/{id}/stream` — original/transcoded with **Range** support
- `GET /api/assets/{id}/download?variant=jpg|raw|both|video` — single or zip
- `POST /api/download` — multi-select zip (path-safe)
- `POST /api/upload` — upload media into the library (path-safe; feeds ingestion)

Internal (worker): `/internal/jobs/*` (see §7.3), behind a shared-secret/token.

All filesystem paths sanitized against the library root (see §12).

---

## 11. CLI subcommands

`main.go` moves from a single `-config` flag to subcommands:
- `serve` — API + UI + watcher/indexer.
- `scan` / `index` — one-shot reconciliation.
- `warm-cache` — pre-generate all thumbnails/posters (+ optionally enqueue enrichment);
  idempotent via content hash.
- `enrich-worker --server=… --token=…` — long-running GPU transform/enrichment worker.
- `update` — self-update (see §13).

---

## 12. Security

- **Path traversal (must-fix, Phase 0):** every request-derived path resolved and
  constrained to the library root via `filepath.Clean` + escape check, or **Go 1.24
  `os.Root`** for traversal-proof opens. Applies to listing, streaming, scaling, download,
  upload.
- **Auth:** minimal single-user gate now (token/session); the internal job API uses a
  shared secret. Real auth/multi-user is later/out of scope.
- Fix `IsDeviceCookieStillValid` (logic is inverted/non-functional) or replace the device
  model as part of the rework.

---

## 13. Deployment: systemd + self-update + releases

- **systemd units** for both roles (`fs-image-manager serve` on the media server,
  `fs-image-manager enrich-worker` on the GPU box); ship unit files + install docs/helper.
- **`update` subcommand:** fetch latest release, **verify checksum**, atomically replace
  the running binary, restart the unit. Implementation via `creativeprojects/go-selfupdate`
  (or equivalent) against GitHub Releases.
- **Release workflow (new):** on `v*` tag push, build versioned, checksummed binaries and
  publish to **GitHub Releases** (today's `build.yml` only uploads an ephemeral, auth-gated
  CI artifact — unusable for `update`). Aligns with existing tags (`v0.01`…`v0.1.6`) and the
  prior tag-triggered release habit.
- **Reconcile with the user's existing external download script** (not in repo) so asset
  naming / URL patterns match. *(Pending input.)*
- Clean up: fix the `webhook-broker-*` glob regression; remove Travis badge/docs and
  docker-helper/NewsCred-S3 remnants; reconsider the custom `go-node` Docker base.

---

## 14. Frontend (fresh build)

- **Vite + React + TypeScript**, embedded into the binary. Replace CRA (sunset) and the
  leftover Aurelia `WebAPI.ts`.
- Component layer: Tailwind + headless kit, or Mantine/MUI (drop niche `precise-ui`).
- Features: **virtualized photo/video grid** (essential at DSLR scale), breadcrumb folder
  nav, lightbox/detail (with video player using range streaming), multi-select → zip
  download, **search bar + timeline**, upload drop-zone. **PWA** for phone use.

---

## 15. Phased delivery

- **Phase 0 — Stop the bleeding:** fix path traversal; fix `webhook-broker-*` CI glob;
  drop Travis/docker-helper remnants; keep the good `main.go` lint cleanup.
- **Phase 1 — Backend foundation:** Go bump; stdlib routing; pure-Go SQLite; sqlc/GORMv2;
  DI struct; single-binary `go:embed`; thumbnail/poster cache.
- **Phase 2 — Media model + ingestion:** asset/file/derivative model; RAW+JPG grouping;
  video listing/poster/range streaming; watcher + 5-min debounce + reconcile;
  `scan`/`warm-cache`.
- **Phase 3 — Index + search:** EXIF/ffprobe extraction → FTS5; search/timeline API + UI.
- **Phase 4 — Job queue + worker:** durable queue; HTTP job API; `enrich-worker`;
  video transcode + RAW-only develop + WebP/AVIF via ffmpeg/GPU.
- **Phase 5 — Enrichment + people recognition:** generic `Enricher` (Ollama default) +
  embeddings/semantic search; **people-recognition subsystem** (`FaceRecognizer`:
  Rekognition Collections by default, local ArcFace alternative) with Person/Face catalog,
  a People view, and cluster-naming UI.
- **Phase 6 — Frontend completion + upload:** full Vite app, upload, PWA.
- **Phase 7 — Ops:** systemd units, release-on-tag workflow, `update` command.

(Phases 0–1 are sequential foundations; later phases can overlap.)

---

## 16. Open items / decisions to confirm

1. **Download script** — share the existing external script so `update` + the release
   workflow match its conventions.
2. **sqlc vs GORM v2** — preference? (Spec recommends sqlc.)
3. **WebP/AVIF on the host** — accept CGO-via-worker-only (recommended) or never (JPEG +
   cache only)?
4. **RAW develop tool** in the worker — libraw/dcraw vs darktable-cli vs ffmpeg (quality
   vs footprint).
5. **Auth depth** for the single-user gate (token vs session vs none-on-LAN).
6. **Face recognition backend** — Rekognition Collections (quality, cloud, uploads faces)
   vs local InsightFace/ArcFace (private, GPU, more build). Spec defaults to Rekognition per
   user's explicit ask; confirm the privacy trade-off is acceptable long-term.
7. Project rename "image" → "media"? (cosmetic; can defer.)
