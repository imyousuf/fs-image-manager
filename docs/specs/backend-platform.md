# Teammate Spec — backend-platform

**Role:** Foundation owner & critical path. You land the skeleton everyone else builds on.
Read `docs/specs/_contracts.md` (you own it) and `docs/TECH_SPEC.md` §4,5,11,12.

## Objective
Modernize the Go foundation and ship the single-binary scaffold, the alias path model, the
DB/migration/sqlc framework, the HTTP server + auth + CLI, and the Phase-0 security fix.

## Owned packages
`main.go`, `internal/{config,server,httpx,db,mediapath,internaltest}`, `sqlc.yaml`,
`internal/db/migrations/0001–0099_*.sql`, `go.mod`/`go.sum`, `go:embed` of the web build.

## Component design
1. **go.mod**: bump to `go 1.24`; remove gorilla/mux, jinzhu/gorm, mattn/go-sqlite3,
   lumberjack-on-file unless needed; add `modernc.org/sqlite`, `pressly/goose/v3`, sqlc
   (tool), `fsnotify` (for ingest, add now). Keep go-ini.
2. **internal/config**: parse the INI in `_contracts.md §7`; expose typed getters +
   `Libraries() []Library` (alias→absolute root, tilde-expanded, validated dir). Error on
   duplicate alias / missing root.
3. **internal/mediapath**: implement `Resolver` (`_contracts.md §2`) using `filepath.Clean`
   + **`os.Root`** for traversal-proof `Open`. Reject relative/alias-less/escaping paths.
   This is the security core — test it hard.
4. **internal/db**: open modernc sqlite (WAL, foreign_keys on); run embedded goose
   migrations at startup; `sqlc.yaml` wiring → `internal/db/store`.
5. **internal/httpx**: JSON encode/decode, error envelope, `MediaPath` query/path parsing
   helpers (validate via mediapath).
6. **internal/server**: `http.Server` with `ServeMux` (1.22 patterns), middleware:
   slog request logging, panic recovery, **bearer-token auth** (`[auth] token`; open if
   empty), and the internal job-API auth shim (shared secret) for jobs-worker to mount.
   Serve embedded frontend from `web/dist` via `go:embed` + `http.FileServerFS`; SPA
   fallback to `index.html`. Provide a `Mux`/route-registration hook so media/search/jobs/
   people teammates can register their handlers.
7. **main.go**: subcommand dispatch (`serve|scan|warm-cache|enrich-worker|update`); wire
   config→db→resolver→server. `update` is a thin stub calling into ops-deploy's package
   (coordinate: define `internal/selfupdate` interface or let ops-deploy own `update`).
8. **internal/internaltest**: shared fixtures (temp dir, temp config w/ two roots, sample
   media copier pulling a few files from `~/Pictures` & `~/Videos`, fakes for
   Cache/Queue/Enricher/FaceRecognizer).
9. **Phase 0 security**: the mediapath resolver replaces all `libraryRoot + userInput`
   concatenation. Remove the broken `IsDeviceCookieStillValid`/device model unless reused.

## Interfaces
Define & export: `mediapath.Resolver`, the route-registration hook, the shared-fixture
helpers, the catalog types are owned by media-pipeline but you may stub `Library`. Consume:
none (you're the base).

## Tests (required)
- mediapath: exhaustive traversal/relative/unknown-alias rejection table; `os.Root` escape
  attempts; valid alias resolution. **This is the highest-value test set in the repo.**
- config: parsing, dup alias, tilde expansion, missing root.
- db: migrations apply on a temp sqlite; sqlc store smoke.
- server: auth middleware (token set/unset), SPA fallback, error envelope shape (httptest).

## Acceptance
`go build ./...` CGO-free; `go test -race ./...` green; `serve` boots against a 2-root temp
config, serves embedded UI placeholder, rejects `pictures/../../etc/passwd`. Other teammates
can import your packages and register routes.

## Verified facts
Module `github.com/imyousuf/fs-image-manager`; test roots `~/Pictures`,`~/Videos`; AWS
profile `imyousuf`/us-east-1 (only relevant to you for config keys).
