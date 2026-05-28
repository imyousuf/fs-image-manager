package ingest_test

import (
	"context"
	"testing"
	"time"
)

// TestWatchDebounceHoldsThenFires: with a short trailing debounce, a file
// written after the watcher starts is NOT catalogued during the quiet period,
// then IS catalogued once the debounce elapses. This validates the
// reset-on-each-event trailing debounce (never act mid-copy).
func TestWatchDebounceHoldsThenFires(t *testing.T) {
	debounce := 400 * time.Millisecond
	ing, fx := newFixture(t, debounce, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = ing.Watch(ctx)
		close(done)
	}()

	// Give the watcher a moment to register watches and run its initial
	// reconcile (the tree is empty at this point).
	time.Sleep(80 * time.Millisecond)

	writeFile(t, fx.roots.Pictures, "live/New.JPG", []byte("freshly-dropped"))

	// Shortly after the event, before the debounce elapses: still not present.
	time.Sleep(120 * time.Millisecond)
	page, _ := fx.repo.ListByDir(ctx, "pictures", "live", "", 0)
	if len(page.Assets) != 0 {
		t.Fatalf("debounce should hold; asset appeared early: %d", len(page.Assets))
	}

	// After the debounce (plus slack) the reconcile fires and catalogs it.
	deadline := time.Now().Add(debounce + 2*time.Second)
	var seen int
	for time.Now().Before(deadline) {
		p, _ := fx.repo.ListByDir(ctx, "pictures", "live", "", 0)
		if len(p.Assets) == 1 {
			seen = 1
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if seen != 1 {
		t.Fatal("debounced reconcile never catalogued the new file")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Watch did not return after context cancel")
	}
}
