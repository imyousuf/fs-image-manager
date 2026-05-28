// Package httpx holds the small HTTP plumbing shared by every handler: JSON
// encode/decode, the error envelope (docs/specs/_contracts.md §5) and helpers
// to parse MediaPath inputs (validated via internal/mediapath).
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/imyousuf/fs-image-manager/internal/mediapath"
)

// maxJSONBody caps decoded request bodies to a sane size for the API.
const maxJSONBody = 4 << 20 // 4 MiB

// ErrorEnvelope is the wire shape for all API errors:
// {"error":{"code":string,"message":string}}.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries the machine code and human message of an error.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The header is already sent; we can only log.
		slog.Error("httpx: encode response", "err", err)
	}
}

// WriteError writes the standard error envelope with the given status, code
// and message.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}

// WriteAPIError maps a Go error to an envelope, choosing the status and code
// from known sentinels (notably mediapath validation errors).
func WriteAPIError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal"
	switch {
	case errors.Is(err, mediapath.ErrUnknownAlias):
		status, code = http.StatusNotFound, "unknown_alias"
	case errors.Is(err, mediapath.ErrEmptyPath),
		errors.Is(err, mediapath.ErrNoAlias),
		errors.Is(err, mediapath.ErrRelativePath),
		errors.Is(err, mediapath.ErrEscapesRoot):
		status, code = http.StatusBadRequest, "invalid_path"
	}
	WriteError(w, status, code, err.Error())
}

// DecodeJSON decodes a JSON request body into dst, rejecting unknown fields
// and oversized bodies.
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("httpx: decode body: %w", err)
	}
	// Ensure there is no trailing content beyond a single JSON value.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("httpx: request body must contain a single JSON object")
	}
	return nil
}

// ParseMediaPath validates a raw "<alias>/<relpath>" string against the
// resolver and returns it as a typed MediaPath. It does not touch the
// filesystem; use the resolver's Resolve/Open for that.
func ParseMediaPath(r mediapath.Resolver, raw string) (mediapath.MediaPath, error) {
	mp := mediapath.MediaPath(raw)
	if _, _, _, err := r.Resolve(mp); err != nil {
		return "", err
	}
	return mp, nil
}
