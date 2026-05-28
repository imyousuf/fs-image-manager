# fs-image-manager web UI

Vite + React + TypeScript single-page app for browsing and searching a multi-library
media collection. The production build is embedded into the Go binary via
`//go:embed all:dist` (see `web/embed.go`) and served by `internal/server` with an
SPA fallback to `index.html`.

## Develop

```sh
npm install
npm run dev      # Vite dev server on :5173, proxies /api -> http://localhost:8080
```

Run `fs-image-manager serve` separately so the proxied `/api` calls resolve. No Go
rebuild is needed while iterating on the UI.

## Build (what the binary embeds)

```sh
npm run build    # tsc -b && vite build -> web/dist
```

`web/dist/index.html` is committed as a placeholder so `go build` resolves on a fresh
checkout; `npm run build` overwrites `web/dist` with the real, hashed assets + PWA
service worker. CI must run `npm run build` before building the Go binary.

## Test

```sh
npm run test:run   # Vitest + React Testing Library, API mocked with MSW
npm run typecheck  # tsc -b --noEmit
```

Component tests live beside their components (`*.test.tsx`); MSW handlers in
`src/test/handlers.ts` implement the HTTP API from `docs/specs/_contracts.md §5` over
fixtures in `src/test/fixtures.ts`, so the UI is tested without a backend.

## Layout

- `src/api/` — typed client (`client.ts`), React Query hooks (`hooks.ts`), contract
  types (`types.ts`), runtime config + bearer token (`config.ts`).
- `src/components/` — presentational + container components (grid, tile, lightbox,
  search bar, timeline, upload, library switcher, breadcrumbs, people view).
- `src/lib/` — asset/media-path helpers.
- `src/App.tsx` — view coordinator (browse / search / people, lightbox, selection).

## Auth

A single optional bearer token (`_contracts.md §5`). The server may inject
`window.__FSIM_CONFIG__.token` into `index.html`, or the user can paste a token via the
gear menu (persisted in `localStorage`). When the server is open (no `[auth] token`),
no header is sent.

## Notes

- All media paths are alias-prefixed `<alias>/<relpath>` and never resolved to disk on
  the client.
- The asset grid is virtualized (`@tanstack/react-virtual`) — mandatory at DSLR scale.
- One tile per logical asset: RAW+JPG bundles render once; videos show a play badge.
- Installable PWA with an offline app shell (`vite-plugin-pwa`); the API is never cached.
