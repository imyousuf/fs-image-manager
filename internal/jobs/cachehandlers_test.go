package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/internaltest"
)

// fakeDerivWriter captures PutDerivative calls for assertion and can be made to
// fail.
type fakeDerivWriter struct {
	calls   []catalog.Derivative
	hashes  []string
	failErr error
}

func (f *fakeDerivWriter) PutDerivative(_ context.Context, d catalog.Derivative, srcHash string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.calls = append(f.calls, d)
	f.hashes = append(f.hashes, srcHash)
	return nil
}

// TestRegisterCacheHandlersStoresDerivative proves that a result posted through
// the registry for a transform kind is written to the cache AND recorded as a
// catalog.Derivative — the behaviour whose absence was the dropped-registry bug.
func TestRegisterCacheHandlersStoresDerivative(t *testing.T) {
	cache := internaltest.NewFakeCache()
	dw := &fakeDerivWriter{}
	reg := NewRegistry()
	RegisterCacheHandlers(reg, cache, dw)

	job := catalog.Job{
		ID:      "j1",
		Kind:    catalog.JobKindConvertImage,
		AssetID: "asset-7",
		Params:  map[string]string{"format": "webp", "width": "1280"},
	}
	result := catalog.JobResult{
		DerivativeKind: "webp",
		Mime:           "image/webp",
		Bytes:          []byte("WEBP-BYTES"),
	}

	if err := reg.Handle(context.Background(), job, result); err != nil {
		t.Fatalf("handle: %v", err)
	}

	// The cache must hold the derivative bytes under the (assetID,kind,params)
	// key the handler computed.
	wantKey := cache.Key("asset-7", "webp", "format=webp&width=1280", "")
	gotBytes, gotMime, ok := cache.Bytes(wantKey)
	if !ok {
		t.Fatalf("derivative not stored in cache under key %q", wantKey)
	}
	if string(gotBytes) != "WEBP-BYTES" || gotMime != "image/webp" {
		t.Fatalf("cached derivative wrong: bytes=%q mime=%q", gotBytes, gotMime)
	}

	// And it must be recorded as a catalog.Derivative pointing at the cache path.
	if len(dw.calls) != 1 {
		t.Fatalf("PutDerivative called %d times, want 1", len(dw.calls))
	}
	d := dw.calls[0]
	if d.AssetID != "asset-7" || d.Kind != "webp" || d.Mime != "image/webp" {
		t.Fatalf("recorded derivative wrong: %+v", d)
	}
	if d.Params != "format=webp&width=1280" {
		t.Fatalf("recorded derivative params %q", d.Params)
	}
	if d.Path == "" {
		t.Fatalf("recorded derivative has no cache path")
	}
}

// TestRegisterCacheHandlersAllTransformKinds confirms every media-transform
// kind gets a handler (and the enrich/face kinds intentionally do not).
func TestRegisterCacheHandlersAllTransformKinds(t *testing.T) {
	reg := NewRegistry()
	RegisterCacheHandlers(reg, internaltest.NewFakeCache(), &fakeDerivWriter{})

	for _, kind := range []string{
		catalog.JobKindTranscodeVideo,
		catalog.JobKindDevelopRAW,
		catalog.JobKindConvertImage,
	} {
		if _, ok := reg.Handler(kind); !ok {
			t.Errorf("no cache handler registered for transform kind %q", kind)
		}
	}
	for _, kind := range []string{catalog.JobKindEnrichAI, catalog.JobKindFaceIndex} {
		if _, ok := reg.Handler(kind); ok {
			t.Errorf("cache handler should NOT be registered for %q (ai-people owns it)", kind)
		}
	}
}

// TestCacheHandlerDerivativeKindFallsBackToJobKind: when the worker omits a
// DerivativeKind, the handler keys the derivative by the job kind instead.
func TestCacheHandlerDerivativeKindFallsBackToJobKind(t *testing.T) {
	cache := internaltest.NewFakeCache()
	dw := &fakeDerivWriter{}
	reg := NewRegistry()
	RegisterCacheHandlers(reg, cache, dw)

	job := catalog.Job{ID: "j2", Kind: catalog.JobKindDevelopRAW, AssetID: "a"}
	result := catalog.JobResult{Mime: "image/jpeg", Bytes: []byte("JPG")} // no DerivativeKind

	if err := reg.Handle(context.Background(), job, result); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(dw.calls) != 1 || dw.calls[0].Kind != catalog.JobKindDevelopRAW {
		t.Fatalf("expected derivative kind to fall back to job kind, got %+v", dw.calls)
	}
}

// TestCacheHandlerRejectsEmptyBytes: a transform result with no derivative
// bytes is an error (these kinds must produce a payload).
func TestCacheHandlerRejectsEmptyBytes(t *testing.T) {
	reg := NewRegistry()
	RegisterCacheHandlers(reg, internaltest.NewFakeCache(), &fakeDerivWriter{})
	err := reg.Handle(context.Background(),
		catalog.Job{Kind: catalog.JobKindConvertImage, AssetID: "a"},
		catalog.JobResult{DerivativeKind: "webp", Mime: "image/webp"}, // no Bytes
	)
	if err == nil {
		t.Fatal("expected error for empty derivative bytes")
	}
}

// TestCacheHandlerPropagatesWriterError: a PutDerivative failure surfaces
// (wrapped) so the job is failed rather than silently completed.
func TestCacheHandlerPropagatesWriterError(t *testing.T) {
	boom := errors.New("db down")
	reg := NewRegistry()
	RegisterCacheHandlers(reg, internaltest.NewFakeCache(), &fakeDerivWriter{failErr: boom})
	err := reg.Handle(context.Background(),
		catalog.Job{Kind: catalog.JobKindConvertImage, AssetID: "a"},
		catalog.JobResult{DerivativeKind: "webp", Mime: "image/webp", Bytes: []byte("x")},
	)
	if !errors.Is(err, boom) {
		t.Fatalf("writer error not propagated: %v", err)
	}
}

// TestParamsStringDeterministic: params render in sorted order regardless of
// map iteration order, so cache keys are stable.
func TestParamsStringDeterministic(t *testing.T) {
	got := paramsString(map[string]string{"z": "1", "a": "2", "m": "3"})
	if got != "a=2&m=3&z=1" {
		t.Fatalf("paramsString = %q, want a=2&m=3&z=1", got)
	}
	if paramsString(nil) != "" {
		t.Fatalf("paramsString(nil) should be empty")
	}
}

// Compile-time proof the production *catalog.Repo would satisfy DerivativeWriter
// is left to media-pipeline's wiring; here the fake stands in for it.
var _ DerivativeWriter = (*fakeDerivWriter)(nil)
