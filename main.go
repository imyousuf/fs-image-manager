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

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/config"
	"github.com/imyousuf/fs-image-manager/internal/costest"
	"github.com/imyousuf/fs-image-manager/internal/db"
	"github.com/imyousuf/fs-image-manager/internal/media"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
	"github.com/imyousuf/fs-image-manager/internal/people"
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
	case "cost-estimate":
		return cmdCostEstimate(rest)
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
  cost-estimate   Estimate the cloud cost (storage + Rekognition) for the media

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

// cmdCostEstimate estimates the cloud cost of the media library: cloud object
// storage (S3/GCS, monthly), AWS Rekognition one-time face indexing (per image,
// tiered), Rekognition ongoing face-metadata storage (monthly), and the $0
// local Ollama line. The actual pricing model lives in internal/costest; this
// command only wires inputs (catalog/people DB, or what-if flags) and output.
//
// Inputs: by default it opens -config + the DB and measures image-asset count,
// total library bytes, and the actual stored face count. Any of -images N,
// -size-gb F, -faces N overrides the corresponding measured value (what-if
// mode). If ALL of -images and -size-gb are supplied, no DB is opened, so the
// command works with no populated catalog.
func cmdCostEstimate(args []string) error {
	fs := flag.NewFlagSet("cost-estimate", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultConfigFilePath, "path to the configuration file")
	imagesFlag := fs.Int64("images", -1, "what-if: override image-asset count (skip DB count)")
	facesFlag := fs.Int64("faces", -1, "what-if: override stored-face count (skip DB count)")
	sizeGBFlag := fs.Float64("size-gb", -1, "what-if: override total library size in GB (skip DB count)")
	avgFaces := fs.Float64("avg-faces", costest.DefaultAvgFacesPerImage, "faces-per-image used to estimate faces when none are indexed")
	freeTier := fs.Bool("free-tier", false, "apply the AWS Rekognition 12-month free-tier allowances")
	asJSON := fs.Bool("json", false, "emit the estimate as JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	// What-if short-circuit: if both -images and -size-gb are given, every input
	// can be supplied by flags, so we never need the DB. -faces, if omitted, is
	// estimated from -images. This is the "works without a populated DB" path.
	if *imagesFlag >= 0 && *sizeGBFlag >= 0 {
		in := costest.Inputs{
			Images:          *imagesFlag,
			SizeBytes:       int64(*sizeGBFlag * 1_000_000_000),
			ImagesAreActual: false,
			SizeIsActual:    false,
		}
		if *facesFlag >= 0 {
			in.Faces = *facesFlag
			in.FacesAreActual = false
		} else {
			in.Faces = costest.EstimateFacesFromImages(in.Images, *avgFaces)
			in.FacesAreActual = false
		}
		return renderCostEstimate(in, *avgFaces, *freeTier, *asJSON)
	}

	// DB-backed path: open config + DB and measure from the catalog/people repos,
	// then apply any individual what-if overrides on top.
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg.DBPath())
	if err != nil {
		return fmt.Errorf("cost-estimate: open db: %w", err)
	}
	defer func() { _ = conn.Close() }()

	aliases := make([]string, 0, len(cfg.Libraries()))
	for _, lib := range cfg.Libraries() {
		aliases = append(aliases, lib.Alias)
	}
	src := costest.RepoSource{
		Aliases:          aliases,
		Assets:           catalog.NewRepo(conn, media.ClassifyExt),
		People:           people.NewRepo(conn),
		Faces:            people.NewRepo(conn),
		AvgFacesPerImage: *avgFaces,
	}
	in, err := src.Collect(ctx)
	if err != nil {
		return err
	}
	// Individual what-if overrides on measured values.
	if *imagesFlag >= 0 {
		in.Images = *imagesFlag
		in.ImagesAreActual = false
	}
	if *sizeGBFlag >= 0 {
		in.SizeBytes = int64(*sizeGBFlag * 1_000_000_000)
		in.SizeIsActual = false
	}
	if *facesFlag >= 0 {
		in.Faces = *facesFlag
		in.FacesAreActual = false
	}
	return renderCostEstimate(in, *avgFaces, *freeTier, *asJSON)
}

// renderCostEstimate computes and writes the estimate to stdout in text or JSON.
func renderCostEstimate(in costest.Inputs, avgFaces float64, freeTier, asJSON bool) error {
	est := costest.Compute(in, costest.Options{AvgFacesPerImage: avgFaces, ApplyFreeTier: freeTier})
	if asJSON {
		return costest.RenderJSON(os.Stdout, est)
	}
	return costest.RenderText(os.Stdout, est)
}
