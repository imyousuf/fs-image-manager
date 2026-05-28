# Teammate Spec — search-index

Read `docs/specs/_contracts.md` and `docs/TECH_SPEC.md` §8. **Depends on** backend-platform
+ media-pipeline (catalog model).

## Objective
Make the library searchable and time-navigable: extract EXIF/video metadata, index it
(columns + FTS5), and expose search + timeline + faceting (including the library **alias**
as a facet). Wire semantic-search storage so ai-people can populate embeddings later.

## Owned packages
`internal/{metadata,search}`, migrations `0200–0299`, queries
`internal/db/queries/search.sql`.

## Component design
1. **metadata**: extract EXIF for images (`rwcarlsen/goexif` or `dsoprea/go-exif`) — capture
   time, camera make/model, lens, dimensions, orientation, GPS (sparse). Video via
   `ffprobe` (optional) — duration, codec, resolution, creation time, GPS. Normalize into a
   `metadata` table keyed by asset ID. Populate `Asset.CapturedAt`. Hook into ingest (called
   by media-pipeline's pipeline or as an `enrich`-adjacent step — coordinate: expose
   `Extract(ctx, asset) (Meta, error)` that ingest calls).
2. **search**: FTS5 virtual table over `name + camera + lens + labels + caption + ocr +
   person names` (labels/caption/ocr/persons filled by ai-people; design columns now, allow
   nulls). Queries: full-text `q`, date range `from/to`, `alias`, `camera`, `person` facets,
   cursor pagination. **timeline**: `GROUP BY date(captured_at)` with counts, filterable by
   alias/range.
3. **Vector storage seam**: a table (or `chromem-go` collection) for embeddings; expose
   `UpsertEmbedding(assetID, []float32)` + `SearchSimilar(vec, k)` for ai-people to call.
   Keep pure-Go (brute-force cosine or chromem-go) — no C extensions.
4. **HTTP handlers** (register via platform hook): `/api/assets/search`,
   `/api/assets/timeline` (shapes in `_contracts.md §5`).

## Interfaces
Implement: `metadata.Extractor`, search repo + handlers, embedding store. Consume: catalog
repo (asset/file info), db store, mediapath (to open files for EXIF), config.

## Tests (required)
- EXIF extraction from real sample JPGs/CR3 (copy a few from `~/Pictures`); ffprobe parse
  (guard if ffprobe absent — `//go:build integration` or skip).
- FTS5 search relevance + faceting (alias/camera/date) on a seeded temp DB.
- timeline aggregation buckets/counts.
- embedding upsert + cosine top-k (deterministic vectors).

## Acceptance
Search returns expected assets by text/date/alias/camera on the seeded corpus; timeline
buckets match; embedding API works against fakes. `go test -race` green.

## Verified facts
DSLR EXIF is rich (date/camera/lens); **GPS is sparse** (prioritize date+camera over map).
Sidecars are not assets — don't index them.
