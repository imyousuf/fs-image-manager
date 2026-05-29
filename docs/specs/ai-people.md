# Teammate Spec — ai-people

Read `docs/specs/_contracts.md` and `docs/TECH_SPEC.md` §9 (esp. §9.2 people recognition).
**Depends on** jobs-worker (runs as jobs) + search-index (embedding store) + AWS.

## Objective
Optional, pluggable enrichment — and the **high-quality people-recognition** subsystem
that is the primary reason for Rekognition. Generic tagging/caption/embeddings (Ollama)
feed search; faces feed a People view.

## Owned packages
`internal/{enrich,people}`, migrations `0400–0499`, queries `internal/db/queries/people.sql`.

## Component design
1. **enrich** (generic, §9.1): `Enricher` impls — `NoopEnricher` (default), `OllamaEnricher`
   (VLM caption/tags + embeddings via Ollama HTTP at `[ai] ollama_url`; private; needs GPU
   box). Results → labels/caption/ocr into search columns; embeddings → search embedding
   store. Runs as `enrich-ai` jobs (register a job result-handler with jobs-worker).
2. **people** (§9.2 — keep it high quality, not gimmicky): `FaceRecognizer` impls —
   - **RekognitionRecognizer (default)**: AWS **Face Collections** — `CreateCollection`,
     `IndexFaces`, `SearchFacesByImage`. Use AWS SDK v2 with profile `imyousuf`, region
     `us-east-1`, collection from `[ai] rekognition_collection`. No GPU.
   - **LocalRecognizer (alternative, documented)**: InsightFace/ArcFace on the GPU worker
     (detection RetinaFace/SCRFD + ArcFace embedding + clustering). Stub + document; full
     impl optional.
   Pipeline (as `face-index` jobs on the worker): detect+embed faces → match against
   collection → assign `Person` or create "unknown" cluster. User names a cluster; new
   faces auto-assign. Cache by face/content hash — only index new assets.
3. **Catalog**: `Person`/`Face` tables (`_contracts.md §3`); People API handlers (register
   via platform hook): `/api/people`, `/api/people/{id}/assets`, `POST /api/people/{id}`
   (rename). Person names flow into the search FTS columns (coordinate with search-index).

## Interfaces
Implement: `Enricher`, `FaceRecognizer`, people repo + handlers, the `enrich-ai`/`face-index`
job result-handlers. Consume: jobs Queue/worker registry, search embedding store + FTS
person column, mediapath, db store, config.

## Tests (required)
- Enricher/FaceRecognizer behind interfaces with **fakes** for the default suite (no
  network). Match/cluster logic, person assignment, unknown handling — deterministic.
- People API handlers (httptest): list, assets-by-person, rename.
- **Real AWS tests are `//go:build integration`** — use profile `imyousuf`; create a
  **throwaway** collection, IndexFaces on a **small sample** (a few hundred max) from
  `~/Pictures`, SearchFaces, then **delete the collection**. Never in default `go test`.

## Acceptance
With fakes: faces detected→matched→grouped into persons, rename works, search-by-person
works. Integration run (lead-gated, cost-aware) recognizes the same person across a sample.
`go test -race` (non-integration) green.

## Verified facts
AWS profile **`imyousuf`**, account 796727287923, **us-east-1**, Rekognition reachable, no
collections yet. Lead has authority to create a bounded throwaway test collection and tear
it down; full-library indexing is a production action. `default` AWS profile is invalid.
