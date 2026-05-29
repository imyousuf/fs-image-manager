package ingest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// reconcileInterval is the period of the safety-net reconcile scan that runs
// regardless of fs events (the watcher is best-effort; reconcile is the source
// of truth, per docs/TECH_SPEC.md §7.2). It is generous: the debounce handles
// the responsive path.
const reconcileInterval = 15 * time.Minute

// Watch runs the live ingestion loop until ctx is cancelled:
//
//   - an initial reconcile of every library, so the catalog is current at
//     startup;
//   - an fsnotify watch of every library root and (recursively) its
//     subdirectories, feeding a TRAILING debounce: each event resets a timer
//     and a reconcile fires only after Debounce of quiet — never mid-copy;
//   - a periodic reconcile (reconcileInterval) as a safety net for events the
//     watcher missed (network FS, overflow, new dirs).
//
// fsnotify is non-recursive, so we add watches for existing subdirectories up
// front and add new ones as CREATE events for directories arrive.
func (in *Ingester) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("ingest: new watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	for _, lib := range in.opts.Resolver.Libraries() {
		if werr := addTree(watcher, lib.Root); werr != nil {
			in.log.Warn("ingest: watch tree", "root", lib.Root, "err", werr)
		}
	}

	// Initial reconcile so we start consistent.
	if _, rerr := in.ReconcileAll(ctx); rerr != nil {
		in.log.Warn("ingest: initial reconcile", "err", rerr)
	}

	// The debounce timer is created stopped; the first event arms it.
	debounce := time.NewTimer(time.Hour)
	if !debounce.Stop() {
		<-debounce.C
	}
	armed := false

	periodic := time.NewTicker(reconcileInterval)
	defer periodic.Stop()

	in.log.Info("ingest: watching", "libraries", len(in.opts.Resolver.Libraries()), "debounce", in.opts.Debounce)

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			in.handleEvent(watcher, ev)
			// Trailing debounce: (re)arm the timer on every event.
			if armed && !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
			debounce.Reset(in.opts.Debounce)
			armed = true

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			in.log.Warn("ingest: watcher error", "err", err)

		case <-debounce.C:
			armed = false
			in.log.Debug("ingest: debounce elapsed, reconciling")
			if _, rerr := in.ReconcileAll(ctx); rerr != nil {
				in.log.Warn("ingest: debounced reconcile", "err", rerr)
			}

		case <-periodic.C:
			in.log.Debug("ingest: periodic reconcile")
			if _, rerr := in.ReconcileAll(ctx); rerr != nil {
				in.log.Warn("ingest: periodic reconcile", "err", rerr)
			}
		}
	}
}

// handleEvent keeps the watch set current: a newly created directory is added
// to the watcher so its future contents are observed (fsnotify is
// non-recursive). All other events just feed the debounce via the caller.
func (in *Ingester) handleEvent(watcher *fsnotify.Watcher, ev fsnotify.Event) {
	if ev.Op&fsnotify.Create == 0 {
		return
	}
	info, err := os.Stat(ev.Name)
	if err != nil || !info.IsDir() {
		return
	}
	if err := addTree(watcher, ev.Name); err != nil {
		in.log.Warn("ingest: add new dir watch", "dir", ev.Name, "err", err)
	}
}

// addTree adds watches for root and every subdirectory beneath it. Hidden
// directories are skipped (mirrors the reconcile walk).
func addTree(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort; skip unreadable dirs
		}
		if !d.IsDir() {
			return nil
		}
		if p != root && isHiddenDir(d.Name()) {
			return filepath.SkipDir
		}
		if aerr := watcher.Add(p); aerr != nil {
			return nil //nolint:nilerr // a dir we cannot watch is non-fatal
		}
		return nil
	})
}
