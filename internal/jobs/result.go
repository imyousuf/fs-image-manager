package jobs

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// Result wire field/query/header names. A worker may post a derivative two
// ways: as multipart/form-data (preferred for large binary payloads alongside
// metadata) or as a raw body with metadata in the query string / headers.
const (
	formFileField  = "file"         // multipart part holding derivative bytes
	formKindField  = "kind"         // derivative kind (e.g. "mp4","developed-jpg")
	formMimeField  = "mime"         // derivative MIME type
	formDataField  = "data"         // JSON-encoded structured result Data
	queryKindParam = "kind"         // raw-body: derivative kind
	headerMimeName = "Content-Type" // raw-body: derivative MIME type
)

// parseResult builds a catalog.JobResult from the request, supporting both the
// multipart and raw-body shapes. It bounds the derivative size to maxResultBytes.
func parseResult(r *http.Request) (catalog.JobResult, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		return parseMultipartResult(r)
	}
	return parseRawResult(r)
}

func parseMultipartResult(r *http.Request) (catalog.JobResult, error) {
	// 16 MiB in-memory threshold; larger parts spill to temp files internally.
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		return catalog.JobResult{}, fmt.Errorf("parse multipart: %w", err)
	}
	res := catalog.JobResult{
		DerivativeKind: r.FormValue(formKindField),
		Mime:           r.FormValue(formMimeField),
	}
	if raw := r.FormValue(formDataField); raw != "" {
		if err := json.Unmarshal([]byte(raw), &res.Data); err != nil {
			return catalog.JobResult{}, fmt.Errorf("decode data field: %w", err)
		}
	}

	file, hdr, err := r.FormFile(formFileField)
	if err == http.ErrMissingFile {
		// A data-only result (e.g. enrich/face metadata with no derivative).
		return res, nil
	}
	if err != nil {
		return catalog.JobResult{}, fmt.Errorf("read file part: %w", err)
	}
	defer func() { _ = file.Close() }()

	bytes, err := io.ReadAll(io.LimitReader(file, maxResultBytes+1))
	if err != nil {
		return catalog.JobResult{}, fmt.Errorf("read derivative bytes: %w", err)
	}
	if int64(len(bytes)) > maxResultBytes {
		return catalog.JobResult{}, fmt.Errorf("derivative exceeds %d bytes", maxResultBytes)
	}
	res.Bytes = bytes
	if res.Mime == "" && hdr != nil {
		res.Mime = hdr.Header.Get("Content-Type")
	}
	return res, nil
}

func parseRawResult(r *http.Request) (catalog.JobResult, error) {
	defer drainAndClose(r.Body)
	bytes, err := io.ReadAll(io.LimitReader(r.Body, maxResultBytes+1))
	if err != nil {
		return catalog.JobResult{}, fmt.Errorf("read body: %w", err)
	}
	if int64(len(bytes)) > maxResultBytes {
		return catalog.JobResult{}, fmt.Errorf("derivative exceeds %d bytes", maxResultBytes)
	}
	return catalog.JobResult{
		DerivativeKind: r.URL.Query().Get(queryKindParam),
		Mime:           r.Header.Get(headerMimeName),
		Bytes:          bytes,
	}, nil
}
