package main

import (
	"context"
	"flag"
	"log/slog"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/config"
	"github.com/imyousuf/fs-image-manager/internal/enrich"
	"github.com/imyousuf/fs-image-manager/internal/people"
	"github.com/imyousuf/fs-image-manager/internal/worker"
)

// This file is the worker-side assembly, the mirror of media_wiring.go's
// wireServe: it is the single package-main place that imports internal/worker
// alongside internal/enrich and internal/people, builds ai-people's worker
// handlers from [ai] config, and registers them on the worker.
//
// It lives here (not in internal/worker) on purpose. internal/enrich and
// internal/people import internal/worker for worker.Handler /
// RegisterWorkerHandler, so internal/worker must not import them back — that
// would be a cycle. The assembly that depends on all three therefore sits in
// package main, exactly as wireServe does for the serve side.

// aiSettings is the slice of config the worker assembly reads to decide which
// AI handlers to register. *config.Config satisfies it; tests pass a small fake
// so handler registration can be exercised without a config file.
type aiSettings interface {
	OllamaURL() string
	RekognitionProfile() string
	RekognitionRegion() string
	RekognitionCollection() string
}

// wireEnrichWorker is the worker subcommand body: it parses the enrich-worker
// flags (plus --config so the worker can read the same [ai] settings as serve),
// loads config, and runs the worker with the AI handlers registered via the
// Setup hook. cmdEnrichWorker delegates to it so main.go stays a thin call,
// matching how cmdServe delegates to wireServe.
func wireEnrichWorker(ctx context.Context, args []string, log *slog.Logger) error {
	fs := flag.NewFlagSet("enrich-worker", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultConfigFilePath, "path to the configuration file")
	serverURL := fs.String("server", "", "media server base URL, e.g. https://media-server:8080")
	secret := fs.String("secret", "", "shared secret for the internal job API")
	concurrency := fs.Int("concurrency", 2, "maximum simultaneously-running jobs")
	leaseSeconds := fs.Int("lease-seconds", 600, "visibility timeout requested per claimed job")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// LoadWorker, not Load: the enrich-worker runs on a separate GPU box with no
	// shared filesystem (TECH_SPEC §3), so it must NOT require or os.Stat the
	// [libraries] roots — it reaches media only through the job API. LoadWorker
	// parses [ai]/[worker]/[database]/[cache]/[http] like Load but skips the
	// [libraries] requirement and root-stat; Libraries() returns empty (unused
	// worker-side). Serve/scan/warm-cache keep using the strict Load.
	cfg, err := config.LoadWorker(*cfgPath)
	if err != nil {
		return err
	}

	return worker.Run(ctx, worker.Config{
		Server: *serverURL,
		Secret: *secret,
		Options: worker.Options{
			Concurrency: *concurrency,
			Lease:       time.Duration(*leaseSeconds) * time.Second,
			Logger:      log,
		},
		// Register ai-people's worker handlers on the freshly-built Worker before
		// it starts claiming. Each is opt-in: absent [ai] config = no handler, so
		// the worker advertises and runs only the transform kinds (the prior
		// behaviour). With config present, it also claims enrich-ai / face-index.
		Setup: func(w *worker.Worker) {
			registerAIWorkerHandlers(ctx, w, cfg, log)
		},
	})
}

// registerAIWorkerHandlers installs ai-people's worker-side handlers on w from
// [ai] config. It is the testable core of the worker AI seam: registration is
// driven entirely by config and degrades gracefully so a worker with no AI
// config (or unreachable AWS) still runs every transform job.
//
//   - enrich-ai  -> enrich.OllamaEnricher when [ai] ollama_url is set; otherwise
//     skipped (logged). The Ollama endpoint is only reached when a job runs, so
//     construction never touches the network here.
//   - face-index -> people.RekognitionRecognizer when [ai] rekognition_collection
//     is set; otherwise skipped (logged). The recognizer is built from the
//     [ai] profile/region/collection and its collection is ensured (idempotent).
//     If AWS config cannot be resolved or the collection cannot be ensured, the
//     handler is NOT registered and the failure is logged rather than fatal, so
//     the worker keeps serving transform jobs.
func registerAIWorkerHandlers(ctx context.Context, w *worker.Worker, cfg aiSettings, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}

	// enrich-ai: Ollama caption/tags/embedding.
	if url := cfg.OllamaURL(); url != "" {
		registerEnrichHandler(w, url)
		log.Info("worker: enrich-ai handler registered", "kind", catalog.JobKindEnrichAI, "ollama_url", url)
	} else {
		log.Info("worker: enrich-ai handler not registered ([ai] ollama_url empty)", "kind", catalog.JobKindEnrichAI)
	}

	// face-index: AWS Rekognition face collection.
	collection := cfg.RekognitionCollection()
	if collection == "" {
		log.Info("worker: face-index handler not registered ([ai] rekognition_collection empty)", "kind", catalog.JobKindFaceIndex)
		return
	}
	if err := registerFaceHandler(ctx, w, cfg); err != nil {
		log.Warn("worker: face-index handler not registered",
			"kind", catalog.JobKindFaceIndex, "collection", collection, "err", err)
		return
	}
	log.Info("worker: face-index handler registered",
		"kind", catalog.JobKindFaceIndex, "collection", collection,
		"profile", cfg.RekognitionProfile(), "region", cfg.RekognitionRegion())
}

// registerEnrichHandler and registerFaceHandler are indirected through function
// variables so the registration/skip/log logic in registerAIWorkerHandlers can
// be unit-tested with fakes (no Ollama, no AWS). Production points them at the
// real ai-people constructors.
var (
	registerEnrichHandler = func(w *worker.Worker, ollamaURL string) {
		enrich.RegisterWorkerHandler(w, enrich.NewOllamaEnricher(enrich.OllamaOptions{BaseURL: ollamaURL}))
	}

	// registerFaceHandler builds the Rekognition recognizer, ensures its collection
	// exists (idempotent network call), and registers the worker-side face-index
	// handler. The worker-side pipeline runs recognition only (no DB, no search) ->
	// nil repo, nil search; the serve side runs Assign.
	registerFaceHandler = func(ctx context.Context, w *worker.Worker, cfg aiSettings) error {
		collection := cfg.RekognitionCollection()
		rec, err := people.NewRekognitionRecognizer(ctx, people.RekognitionOptions{
			Profile:    cfg.RekognitionProfile(),
			Region:     cfg.RekognitionRegion(),
			Collection: collection,
		})
		if err != nil {
			return err
		}
		if err := rec.EnsureCollection(ctx); err != nil {
			return err
		}
		people.RegisterWorkerHandler(w, people.NewPipeline(rec, nil, nil, collection))
		return nil
	}
)

// Compile-time contract check: the real *config.Config supplies the [ai]
// settings the worker assembly reads. If config-platform renames an [ai]
// accessor, this fails here rather than only at the wireEnrichWorker call site.
var _ aiSettings = (*config.Config)(nil)
