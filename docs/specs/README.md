# Per-teammate specs — fs-image-manager modernization

These specs decompose the master spec (`../TECH_SPEC.md`) across a 7-agent team. Start with
`_contracts.md` — it defines the seams every teammate codes against.

| Spec | Role | Owns | Depends on |
|---|---|---|---|
| [_contracts.md](_contracts.md) | shared seams (owned by platform) | path model, types, interfaces, API, config, DB convention | — |
| [backend-platform.md](backend-platform.md) | foundation / critical path | `main.go`, config, server, db, mediapath, httpx | — |
| [media-pipeline.md](media-pipeline.md) | media model + browsing + ingest | catalog, media, cache, ingest | platform |
| [search-index.md](search-index.md) | search + timeline + metadata | metadata, search | platform, media |
| [jobs-worker.md](jobs-worker.md) | job queue + GPU transform worker | jobs, worker, transform | platform, media |
| [ai-people.md](ai-people.md) | enrichment + people recognition | enrich, people | jobs-worker, search, AWS |
| [frontend.md](frontend.md) | embedded Vite/React UI | `web/` | API contract |
| [ops-deploy.md](ops-deploy.md) | CI, releases, systemd, update | `.github/`, `deploy/`, `Makefile`, selfupdate | platform (update) |

**Working rules:** shared tree, foundation-first; edit only your packages; migrations use
your number range; `go.mod` changes via platform; tests ship with code; CI gates merges.
Execution is autonomous — the lead sequences via the team task list, integrates, and
validates (Go/Vitest suites + ATR for the UI).
