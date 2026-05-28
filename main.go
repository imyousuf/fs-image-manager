// Command fs-image-manager is the single binary for the media manager. It
// dispatches subcommands (serve, scan, warm-cache, enrich-worker, update);
// see docs/specs/_contracts.md §8. This file owns CLI wiring only — feature
// behaviour lives in the internal packages owned by each teammate.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/imyousuf/fs-image-manager/internal/config"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
	"github.com/imyousuf/fs-image-manager/internal/selfupdate"
	"github.com/imyousuf/fs-image-manager/internal/server"
	"github.com/imyousuf/fs-image-manager/web"
)

// version is the running binary's version, build-stamped via
// -ldflags "-X main.version=<v>" (set by the Makefile / release workflow).
// It defaults to "dev" for un-stamped local builds.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run dispatches the subcommand. It returns an error rather than exiting so it
// is testable.
func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("no subcommand given")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return cmdServe(rest)
	case "scan":
		return cmdScan(rest)
	case "warm-cache":
		return cmdWarmCache(rest)
	case "enrich-worker":
		return cmdEnrichWorker(rest)
	case "update":
		return cmdUpdate(rest)
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `fs-image-manager <command> [flags]

Commands:
  serve           Run the HTTP API + embedded UI + ingestion watcher
  scan            One-shot reconciliation scan of the libraries
  warm-cache      Pre-generate thumbnails/posters for the libraries
  enrich-worker   Run the heavy transform/enrichment worker (GPU box)
  update          Self-update the binary from GitHub Releases

Run "fs-image-manager <command> -h" for command-specific flags.
`)
}

// loadConfig parses the shared -config flag for a subcommand and loads config.
func loadConfig(name string, args []string) (*config.Config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultConfigFilePath, "path to the configuration file")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func cmdServe(args []string) error {
	cfg, err := loadConfig("serve", args)
	if err != nil {
		return err
	}
	log := newLogger()
	slog.SetDefault(log)

	ctx, cancel := signalContext()
	defer cancel()

	resolver, err := mediapath.NewResolver(cfg.Libraries())
	if err != nil {
		return fmt.Errorf("serve: build resolver: %w", err)
	}

	conn, err := db.Open(ctx, cfg.DBPath())
	if err != nil {
		return fmt.Errorf("serve: open db: %w", err)
	}
	defer func() { _ = conn.Close() }()

	srv := server.New(server.Options{
		Addr:         cfg.Listener(),
		AuthToken:    cfg.AuthToken(),
		WorkerSecret: cfg.WorkerSecret(),
		Resolver:     resolver,
		FrontendFS:   web.Dist(),
		Logger:       log,
	})

	// Single media-pipeline-owned assembly: job queue, media routes, internal
	// job API + cache-result-handlers, search indexer + routes, and the ingest
	// watcher. wireServe returns a stop func that shuts the watcher down cleanly.
	stop, err := wireServe(ctx, cfg, conn, resolver, srv, log)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	defer stop()

	return srv.Start(ctx)
}

// cmdScan runs a one-shot reconciliation of every library against the catalog
// (docs/specs/media-pipeline.md §6): new/changed files are grouped and
// upserted, vanished files pruned. It does not start the watcher.
func cmdScan(args []string) error {
	cfg, err := loadConfig("scan", args)
	if err != nil {
		return err
	}
	log := newLogger()
	slog.SetDefault(log)

	ctx, cancel := signalContext()
	defer cancel()

	resolver, err := mediapath.NewResolver(cfg.Libraries())
	if err != nil {
		return fmt.Errorf("scan: build resolver: %w", err)
	}
	conn, err := db.Open(ctx, cfg.DBPath())
	if err != nil {
		return fmt.Errorf("scan: open db: %w", err)
	}
	defer func() { _ = conn.Close() }()

	stack, err := buildMediaStack(cfg, conn, resolver, nil, log)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	res, err := stack.ingester.ReconcileAll(ctx)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	log.Info("scan complete",
		"added", res.Added, "modified", res.Modified, "deleted", res.Deleted,
		"skipped", res.Skipped, "assets", res.Assets, "enqueued", res.Enqueued)

	// Backfill: index existing assets that predate the metadata seam (or were
	// catalogued while no indexer was wired). Idempotent; logged, never fatal.
	if r, berr := stack.indexer.Backfill(ctx, resolver, stack.repo, log); berr != nil {
		log.Warn("scan: metadata backfill", "err", berr)
	} else {
		log.Info("metadata backfill", "indexed", r.Indexed, "failed", r.Failed)
	}
	return nil
}

// cmdWarmCache pre-generates thumbnails/posters for every library, idempotent by
// source content hash (docs/specs/media-pipeline.md §6). It first reconciles so
// the catalog is current, then warms the cache.
func cmdWarmCache(args []string) error {
	cfg, err := loadConfig("warm-cache", args)
	if err != nil {
		return err
	}
	log := newLogger()
	slog.SetDefault(log)

	ctx, cancel := signalContext()
	defer cancel()

	resolver, err := mediapath.NewResolver(cfg.Libraries())
	if err != nil {
		return fmt.Errorf("warm-cache: build resolver: %w", err)
	}
	conn, err := db.Open(ctx, cfg.DBPath())
	if err != nil {
		return fmt.Errorf("warm-cache: open db: %w", err)
	}
	defer func() { _ = conn.Close() }()

	stack, err := buildMediaStack(cfg, conn, resolver, nil, log)
	if err != nil {
		return fmt.Errorf("warm-cache: %w", err)
	}
	// Reconcile first so newly-arrived files are catalogued before warming.
	if _, rerr := stack.ingester.ReconcileAll(ctx); rerr != nil {
		return fmt.Errorf("warm-cache: reconcile: %w", rerr)
	}
	// Backfill metadata for any not-yet-indexed assets while we're scanning.
	if r, berr := stack.indexer.Backfill(ctx, resolver, stack.repo, log); berr != nil {
		log.Warn("warm-cache: metadata backfill", "err", berr)
	} else {
		log.Info("metadata backfill", "indexed", r.Indexed, "failed", r.Failed)
	}
	warmer := media.NewWarmer(resolver, stack.repo, stack.cache, log)
	res, err := warmer.WarmAll(ctx)
	if err != nil {
		return fmt.Errorf("warm-cache: %w", err)
	}
	log.Info("warm-cache complete", "assets", res.Assets, "generated", res.Generated, "hits", res.Hits)
	return nil
}

func cmdEnrichWorker(args []string) error {
	log := newLogger()
	slog.SetDefault(log)

	ctx, cancel := signalContext()
	defer cancel()

	// wireEnrichWorker (worker_wiring.go, jobs-worker-owned) parses the
	// enrich-worker flags + -config, then runs the worker with a Setup hook that
	// registers ai-people's enrich-ai + face-index handlers from the [ai] config.
	// This mirrors the cmdServe -> wireServe composition seam.
	return wireEnrichWorker(ctx, args, log)
}

func cmdUpdate(args []string) error {
	ctx, cancel := signalContext()
	defer cancel()
	return selfupdate.Run(ctx, args, version)
}
