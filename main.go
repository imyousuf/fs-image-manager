// Command fs-image-manager is the single binary for the media manager. It
// dispatches subcommands (serve, scan, warm-cache, enrich-worker, update);
// see docs/specs/_contracts.md §8. This file owns CLI wiring only — feature
// behaviour lives in the internal packages owned by each teammate.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/imyousuf/fs-image-manager/internal/config"
	"github.com/imyousuf/fs-image-manager/internal/costest"
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
// local Ollama line. The pricing model lives in internal/costest; this command
// only wires inputs (a filesystem walk) and output.
//
// Inputs are PATH-BASED and need no database:
//
//   - One or more positional PATH arguments -> walk those paths.
//   - No path arguments -> load -config and walk the configured [libraries]
//     absolute roots ("by default, take the configured paths").
//
// The walk counts display-image files (.jpg/.jpeg/.png/.heic — not RAW, not
// video, so RAW+JPG pairs are not double-counted) as Rekognition face-index
// candidates, and sums the bytes of every file under the path(s) for cloud
// storage. Faces are not on disk, so the face count is ESTIMATED as
// images * -avg-faces and labelled as such.
//
// The numeric what-if flags (-images/-faces/-size-gb) are OPTIONAL extras that
// override the corresponding walked value; a path (or the configured paths) is
// otherwise all that is needed.
func cmdCostEstimate(args []string) error {
	fs := flag.NewFlagSet("cost-estimate", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `fs-image-manager cost-estimate [flags] [path ...]

Estimate the cloud cost (storage + AWS Rekognition) for media under a path.

  cost-estimate /photos/2024        estimate for one path tree
  cost-estimate /a /b               estimate across several paths
  cost-estimate                     walk the configured [libraries] roots

Flags:
`)
		fs.PrintDefaults()
	}
	cfgPath := fs.String("config", config.DefaultConfigFilePath, "config file (used to find library roots when no path is given)")
	avgFaces := fs.Float64("avg-faces", costest.DefaultAvgFacesPerImage, "faces-per-image used to estimate the stored-face count")
	freeTier := fs.Bool("free-tier", false, "apply the AWS Rekognition 12-month free-tier allowances")
	asJSON := fs.Bool("json", false, "emit the estimate as JSON instead of text")
	imagesFlag := fs.Int64("images", -1, "optional what-if: override the walked display-image count")
	facesFlag := fs.Int64("faces", -1, "optional what-if: override the estimated stored-face count")
	sizeGBFlag := fs.Float64("size-gb", -1, "optional what-if: override the walked total size in GB")

	// Parse flags and positional PATH args in any order. The stdlib flag package
	// stops at the first non-flag token, so we loop: parse, peel off the leading
	// positional(s) until the next flag, then parse the rest. This lets a path be
	// the obvious primary input whether flags come before or after it
	// (e.g. "cost-estimate /photos -json" and "cost-estimate -json /photos").
	var roots []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			// -h/-help prints usage and asks to stop; that is not a failure.
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		// Consume leading positionals (non-flag tokens) as paths.
		for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			roots = append(roots, rest[0])
			rest = rest[1:]
		}
		if len(rest) == 0 {
			break
		}
	}

	ctx, cancel := signalContext()
	defer cancel()

	// Resolve the roots to walk: positional path args take precedence; otherwise
	// fall back to the configured [libraries] roots ("by default the configured
	// paths"). No DB is ever opened.
	if len(roots) == 0 {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return err
		}
		for _, lib := range cfg.Libraries() {
			roots = append(roots, lib.Root)
		}
	}
	if err := costest.ValidateRoots(roots); err != nil {
		return err
	}

	in, err := costest.FSSource{Roots: roots, AvgFacesPerImage: *avgFaces}.Collect(ctx)
	if err != nil {
		return err
	}

	// Optional numeric what-if overrides on the walked values.
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
