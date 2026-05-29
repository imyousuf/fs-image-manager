// Package migrations embeds the goose SQL migrations for the whole
// application. Files are numbered per-role to avoid collisions (see
// docs/specs/_contracts.md §6): platform 0001–0099, media-pipeline 0100–0199,
// search-index 0200–0299, jobs-worker 0300–0399, ai-people 0400–0499.
package migrations

import "embed"

// FS holds every migration, applied in lexical (numeric) order by goose.
//
//go:embed *.sql
var FS embed.FS
