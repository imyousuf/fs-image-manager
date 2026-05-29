# Teammate Spec — frontend

Read `docs/specs/_contracts.md` (esp. §5 HTTP API) and `docs/TECH_SPEC.md` §14,10.
**Depends on** the API contract (mock with MSW early; integrate as endpoints land).

## Objective
A fresh, fast, embeddable web UI for browsing/searching a multi-library media collection,
replacing the empty CRA shell.

## Owned packages
`web/` — a new **Vite + React + TypeScript** app whose production build is embedded by
backend-platform via `go:embed` (build output to `web/dist`, matched to platform's embed
path — coordinate the exact dir).

## Component design
1. **Tooling**: Vite + React 18/19 + TS. Drop CRA, `precise-ui`, and the leftover Aurelia
   `WebAPI.ts`. Styling: Tailwind (or Mantine) — pick one, keep it lean. Configure Vite dev
   proxy `/api` → local `serve`.
2. **API client**: typed client generated from / matching `_contracts.md §5`. Bearer token
   support (config-provided). All media paths are alias-prefixed.
3. **Views**:
   - **Library switcher** (from `/api/libraries`) + breadcrumb folder nav (`/api/assets?path=`).
   - **Virtualized grid** (essential at DSLR scale) of asset thumbnails (one tile per asset;
     RAW+JPG shows once; video tiles show a play badge over the poster).
   - **Lightbox/detail**: preview image or **video player** (uses `/stream`, Range);
     show metadata; download menu (jpg/raw/both/video).
   - **Search bar + facets** (text/date/camera/alias/person) and a **timeline** view.
   - **Upload** drop-zone (`/api/upload?alias=&dir=`).
   - **People view** (`/api/people`) with cluster naming.
   - **PWA** (installable, offline shell) for phone use.

## Interfaces
Consume: the HTTP API (`_contracts.md §5`). Produce: `web/dist` for embedding.

## Tests (required)
- **Vitest + React Testing Library** unit/component tests for grid, lightbox/player, search,
  timeline, upload, library switcher, People view.
- **MSW** mocks the API from the contract so components are tested without a backend.
- Keep components testable (presentational vs data). Lead runs ATR e2e separately.

## Acceptance
`npm run build` + `vitest run` + `tsc` green. Built app embeds and serves from the binary.
Lead-driven ATR pass (browse both libraries, RAW+JPG once, video play, search+timeline,
upload, People) succeeds.

## Verified facts
Two test libraries: `pictures`→`~/Pictures`, `videos`→`~/Videos`. Large libraries ⇒
virtualization is mandatory. Map view low priority (GPS sparse) — date/camera/person first.
