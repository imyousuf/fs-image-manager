// Package server wires the public HTTP API, the internal job API and the
// embedded SPA into a single http.Server. It owns routing (stdlib ServeMux,
// 1.22 method+pattern syntax), the middleware chain (request logging, panic
// recovery, bearer-token auth) and the route-registration hooks that other
// teammates use to mount their handlers.
package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/httpx"
	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// Options configures a Server.
type Options struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string
	// AuthToken guards the public /api routes via bearer auth. Empty = open.
	AuthToken string
	// WorkerSecret guards the internal /internal/jobs routes. Empty disables
	// the internal API entirely (no worker connectivity).
	WorkerSecret string
	// Resolver is the sanctioned media-path resolver, exposed to handlers.
	Resolver mediapath.Resolver
	// FrontendFS is the embedded production SPA (index.html at the root). If
	// nil, SPA serving is disabled and unknown routes 404.
	FrontendFS fs.FS
	// Logger is used by middleware; defaults to slog.Default().
	Logger *slog.Logger
}

// Server is the application HTTP server plus the muxes feature teammates
// register routes on.
type Server struct {
	opts Options
	log  *slog.Logger

	// apiMux holds public /api/* routes; bearer auth wraps the whole group.
	apiMux *http.ServeMux
	// internalMux holds /internal/jobs/* routes; shared-secret auth wraps it.
	internalMux *http.ServeMux
	// root is the top-level mux that dispatches /api, /internal and the SPA.
	root *http.ServeMux

	httpServer *http.Server
}

// New constructs a Server. Call the API/Internal registration hooks to mount
// handlers, then Start.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	s := &Server{
		opts:        opts,
		log:         opts.Logger,
		apiMux:      http.NewServeMux(),
		internalMux: http.NewServeMux(),
		root:        http.NewServeMux(),
	}
	s.buildRoutes()
	return s
}

// Resolver exposes the media-path resolver to registered handlers.
func (s *Server) Resolver() mediapath.Resolver { return s.opts.Resolver }

// API returns the mux for public /api routes. Register paths *without* the
// "/api" prefix — e.g. mux.HandleFunc("GET /libraries", h). Bearer auth (if a
// token is configured) and the standard middleware wrap the whole group.
func (s *Server) API() *http.ServeMux { return s.apiMux }

// Internal returns the mux for /internal/jobs routes used by the worker.
// Register paths without the "/internal/jobs" prefix — e.g.
// mux.HandleFunc("POST /claim", h). The shared-secret shim wraps the group.
// Registering on it has no effect if WorkerSecret is empty.
func (s *Server) Internal() *http.ServeMux { return s.internalMux }

// buildRoutes assembles the top-level mux: the SPA/static handler as the
// catch-all, the auth-wrapped API group under /api/, and the secret-wrapped
// internal group under /internal/jobs/.
func (s *Server) buildRoutes() {
	// Public API group: strip the /api prefix, wrap in bearer auth.
	apiHandler := http.StripPrefix("/api", s.apiMux)
	apiHandler = bearerAuth(s.opts.AuthToken)(apiHandler)
	s.root.Handle("/api/", apiHandler)

	// Internal job API group (only if a secret is set).
	if s.opts.WorkerSecret != "" {
		internalHandler := http.StripPrefix("/internal/jobs", s.internalMux)
		internalHandler = sharedSecretAuth(s.opts.WorkerSecret)(internalHandler)
		s.root.Handle("/internal/jobs/", internalHandler)
	}

	// Everything else: the embedded SPA, or a JSON 404 if no frontend.
	if s.opts.FrontendFS != nil {
		s.root.Handle("/", spaHandler(s.opts.FrontendFS))
	} else {
		s.root.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "no frontend embedded")
		})
	}
}

// handler returns the fully wrapped root handler (recovery + request logging
// around the dispatching mux). Exposed for httptest in unit tests.
func (s *Server) handler() http.Handler {
	var h http.Handler = s.root
	h = requestLogger(s.log)(h)
	h = recoverPanic(s.log)(h)
	return h
}

// Start runs the HTTP server until ctx is cancelled, then performs a graceful
// shutdown with a short timeout. It blocks; run it in a goroutine if needed.
func (s *Server) Start(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:              s.opts.Addr,
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", "addr", s.opts.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.shutdown()
	}
}

func (s *Server) shutdown() error {
	s.log.Info("http server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	s.log.Info("http server stopped")
	return nil
}
