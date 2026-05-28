package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
)

func TestRegistryHandleMissingIsNoop(t *testing.T) {
	r := NewRegistry()
	// No handler registered for the kind: Handle is a no-op, not an error.
	if err := r.Handle(context.Background(), catalog.Job{Kind: catalog.JobKindConvertImage}, catalog.JobResult{}); err != nil {
		t.Fatalf("expected nil for missing handler, got %v", err)
	}
}

func TestRegistryHandleInvokesAndWrapsError(t *testing.T) {
	r := NewRegistry()
	want := errors.New("store failed")
	var got catalog.JobResult
	r.Register(catalog.JobKindConvertImage, func(_ context.Context, _ catalog.Job, res catalog.JobResult) error {
		got = res
		return want
	})

	err := r.Handle(context.Background(), catalog.Job{Kind: catalog.JobKindConvertImage}, catalog.JobResult{DerivativeKind: "webp"})
	if !errors.Is(err, want) {
		t.Fatalf("error not wrapped: %v", err)
	}
	if got.DerivativeKind != "webp" {
		t.Fatalf("handler did not receive result: %+v", got)
	}
}

func TestRegistryReplaceAndClear(t *testing.T) {
	r := NewRegistry()
	r.Register("k", func(context.Context, catalog.Job, catalog.JobResult) error { return nil })
	if _, ok := r.Handler("k"); !ok {
		t.Fatal("handler not registered")
	}
	r.Register("k", nil) // clear
	if _, ok := r.Handler("k"); ok {
		t.Fatal("handler not cleared by nil register")
	}
}
