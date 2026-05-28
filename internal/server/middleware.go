package server

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/httpx"
)

// middleware is the standard wrapping shape used throughout the server.
type middleware func(http.Handler) http.Handler

// statusRecorder captures the response status code for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer so range
// streaming / flushing still work through the recorder.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// requestLogger logs one structured line per request with method, path,
// status and duration.
func requestLogger(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			)
		})
	}
}

// recoverPanic converts a panic in any handler into a 500 error envelope and
// logs it, so a single bad request can never crash the server.
func recoverPanic(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered", "err", rec, "path", r.URL.Path)
					httpx.WriteError(w, http.StatusInternalServerError, "internal", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// bearerAuth enforces "Authorization: Bearer <token>" when token is non-empty.
// An empty token means the API is open (LAN single-user mode).
func bearerAuth(token string) middleware {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented, ok := bearerToken(r)
			if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sharedSecretAuth guards the internal job API. The worker presents the secret
// via "Authorization: Bearer <secret>" or the "X-Worker-Secret" header.
func sharedSecretAuth(secret string) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented, ok := bearerToken(r)
			if !ok {
				presented = r.Header.Get("X-Worker-Secret")
			}
			if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) != 1 {
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "invalid worker secret")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header. The scheme match is case-insensitive per RFC 7235.
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}
