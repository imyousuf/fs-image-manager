# Teammate Spec — ops-deploy

Read `docs/specs/_contracts.md` and `docs/TECH_SPEC.md` §13. **Mostly independent** — start
the CI cleanup immediately; coordinate the `update` command with backend-platform.

## Objective
Build/release/deploy: fix and modernize CI, publish installable releases, provide systemd
units, and implement the self-`update` command. Make tests gate everything.

## Owned packages / files
`.github/workflows/*`, `Dockerfile`, `Makefile`, `deploy/` (systemd units + install docs),
`internal/selfupdate` (+ the `update` subcommand body, wired into platform's dispatch).

## Component design
1. **Phase-0 CI fixes (do first)**: in `.github/workflows/build.yml` revert the artifact
   glob from `webhook-broker-*` back to `fs-image-manager-*`. Remove Travis remnants
   (README badge, docs), docker-helper/NewsCred S3 push in the Makefile. Reconsider the
   custom `go-node` Docker base.
2. **CI (PR gate)**: workflow running `go build ./...`, `go test -race ./...`,
   `golangci-lint`, `sqlc diff` (generated code up-to-date), and frontend `npm ci` +
   `vitest run` + `tsc` + `vite build`. **Block merge on failure.**
3. **Release workflow (new)**: on `v*` tag push, cross-build CGO-free binaries
   (linux/amd64+arm64), name `fs-image-manager_<version>_<os>_<arch>.tar.gz`, generate
   `SHA256SUMS`, publish to **GitHub Releases**. (Frontend built & embedded first.)
4. **`internal/selfupdate` + `update` cmd**: query latest GitHub Release (e.g.
   `creativeprojects/go-selfupdate`), verify checksum, atomically replace the running
   binary, then restart the systemd unit. Reconcile asset naming with the user's external
   download script if later provided.
5. **systemd**: `deploy/fs-image-manager.service` (serve, on the media server) and
   `deploy/fs-image-manager-worker.service` (enrich-worker, on the GPU box) + install docs.
6. **Makefile**: modern targets — `build` (frontend→embed→go build), `test` (both suites),
   `lint`, `release`. Drop the `setup-docker-dev`/travis targets.

## Interfaces
Produce: CI, releases, systemd units, `update`. Consume: platform's CLI dispatch hook for
`update`; the frontend build output location for embedding.

## Tests (required)
- `internal/selfupdate`: checksum verification + atomic-replace logic against a fake release
  server/temp files (no real network in unit tests; real GH in `//go:build integration`).
- CI workflow validated by a dry run (act or a scratch branch) — at minimum lint the YAML.

## Acceptance
CI builds + runs both test suites + lints and gates PRs; a tag produces downloadable,
checksummed release assets; `update` self-replaces against a fake release in tests; systemd
units start `serve`/`enrich-worker`. The `webhook-broker-*` regression is gone.

## Verified facts
Existing tags `v0.01…v0.1.6`; current `build.yml` only uploads an ephemeral artifact +
pushes Docker — no Releases, no in-repo download script (user's is external). CGO-free build.
