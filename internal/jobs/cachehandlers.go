package jobs

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

// DerivativeWriter is the minimal slice of the catalog repository the cache
// result-handlers need: recording a produced derivative for an asset. It is
// declared here (rather than imported from media-pipeline) deliberately — the
// jobs package must not import internal/catalog's repo owner or internal/media,
// or it would form an import cycle (media enqueues via catalog.Queue, which
// jobs implements). The media-pipeline *catalog.Repo satisfies this interface,
// so serve can pass its repo straight in.
type DerivativeWriter interface {
	// PutDerivative records d (keyed by srcHash) as the cached output of a job.
	PutDerivative(ctx context.Context, d catalog.Derivative, srcHash string) error
}

// RegisterCacheHandlers wires the media-transform result handlers onto reg: for
// each of transcode-video, develop-raw and convert-image, a posted result is
// stored in the derivative cache and recorded against its asset. This is the
// glue the serve process installs at startup so a worker's derivatives are
// actually persisted (without it, results would be silently dropped). The
// enrich/face kinds are registered separately by ai-people.
//
// Both cache and deriv must be non-nil; reg must be non-nil.
func RegisterCacheHandlers(reg *Registry, cache catalog.Cache, deriv DerivativeWriter) {
	h := cacheResultHandler(cache, deriv)
	reg.Register(catalog.JobKindTranscodeVideo, h)
	reg.Register(catalog.JobKindDevelopRAW, h)
	reg.Register(catalog.JobKindConvertImage, h)
}

// cacheResultHandler builds the ResultHandler shared by the media-transform
// kinds: store the derivative bytes in the cache, then record the derivative
// row pointing at the cached path. The derivative kind comes from the worker's
// result (e.g. "mp4","developed-jpg","webp"), falling back to the job kind if
// the worker did not set one.
func cacheResultHandler(cache catalog.Cache, deriv DerivativeWriter) ResultHandler {
	return func(ctx context.Context, job catalog.Job, result catalog.JobResult) error {
		if len(result.Bytes) == 0 {
			return fmt.Errorf("jobs: %s result for asset %s has no derivative bytes", job.Kind, job.AssetID)
		}
		derivKind := result.DerivativeKind
		if derivKind == "" {
			derivKind = job.Kind
		}
		params := paramsString(job.Params)

		// srcHash is empty here: the worker fetched the source via the job API,
		// so the host does not re-hash it. The (assetID, kind, params) tuple is
		// the stable identity for a worker-produced derivative.
		const srcHash = ""
		key := cache.Key(job.AssetID, derivKind, params, srcHash)
		path, err := cache.Put(key, bytes.NewReader(result.Bytes), result.Mime)
		if err != nil {
			return fmt.Errorf("jobs: cache %s derivative for %s: %w", derivKind, job.AssetID, err)
		}

		if err := deriv.PutDerivative(ctx, catalog.Derivative{
			AssetID: job.AssetID,
			Kind:    derivKind,
			Params:  params,
			Path:    path,
			Mime:    result.Mime,
		}, srcHash); err != nil {
			return fmt.Errorf("jobs: record %s derivative for %s: %w", derivKind, job.AssetID, err)
		}
		return nil
	}
}

// paramsString renders a job's params map as a stable, deterministic string
// (sorted "k=v" pairs joined by "&") so the same params always produce the same
// cache key and derivative identity. An empty/nil map yields "".
func paramsString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}
