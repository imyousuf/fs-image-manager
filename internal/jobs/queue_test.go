package jobs

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/catalog"
	"github.com/imyousuf/fs-image-manager/internal/db"
)

// openTestDB opens a fresh on-disk SQLite database (migrations applied) under a
// temp dir. On-disk (not :memory:) so the restart-durability test can reopen
// the same file with a brand-new connection.
func openTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs_test.db")
	conn, err := db.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, path
}

func TestEnqueueAndClaim(t *testing.T) {
	conn, _ := openTestDB(t)
	q := NewQueue(conn)
	ctx := context.Background()

	job, err := q.Enqueue(ctx, catalog.JobKindConvertImage, "asset1", "pictures/a.cr3", map[string]string{"format": "webp"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.ID == "" || job.Status != statusPending {
		t.Fatalf("unexpected enqueued job: %+v", job)
	}

	claimed, err := q.Claim(ctx, nil, time.Minute, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("want 1 claimed, got %d", len(claimed))
	}
	if claimed[0].ID != job.ID {
		t.Fatalf("claimed wrong job: %s != %s", claimed[0].ID, job.ID)
	}
	if claimed[0].Params["format"] != "webp" {
		t.Fatalf("params not round-tripped: %+v", claimed[0].Params)
	}

	// A second claim should find nothing (the job is leased).
	again, err := q.Claim(ctx, nil, time.Minute, 10)
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected no jobs on second claim, got %d", len(again))
	}
}

func TestClaimKindFilter(t *testing.T) {
	conn, _ := openTestDB(t)
	q := NewQueue(conn)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, catalog.JobKindTranscodeVideo, "v1", "videos/a.mov", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(ctx, catalog.JobKindDevelopRAW, "r1", "pictures/a.cr3", nil); err != nil {
		t.Fatal(err)
	}

	// Claim only develop-raw: should get the RAW job, leave the video pending.
	claimed, err := q.Claim(ctx, []string{catalog.JobKindDevelopRAW}, time.Minute, 5)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Kind != catalog.JobKindDevelopRAW {
		t.Fatalf("want one develop-raw job, got %+v", claimed)
	}

	pending, err := q.CountByStatus(ctx, statusPending)
	if err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("want 1 still pending (the video), got %d", pending)
	}
}

func TestLeaseExpiryReclaim(t *testing.T) {
	conn, _ := openTestDB(t)
	q := NewQueue(conn)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, catalog.JobKindConvertImage, "a", "pictures/a.jpg", nil); err != nil {
		t.Fatal(err)
	}

	// Claim with a 1-second lease.
	first, err := q.Claim(ctx, nil, time.Second, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %v len=%d", err, len(first))
	}

	// Immediately re-claiming finds nothing (lease still valid).
	if mid, _ := q.Claim(ctx, nil, time.Second, 1); len(mid) != 0 {
		t.Fatalf("expected lease to block reclaim, got %d", len(mid))
	}

	// Wait for the lease (datetime granularity is whole seconds) to lapse.
	deadline := time.Now().Add(5 * time.Second)
	var reclaimed []catalog.Job
	for time.Now().Before(deadline) {
		reclaimed, err = q.Claim(ctx, nil, time.Minute, 1)
		if err != nil {
			t.Fatalf("reclaim: %v", err)
		}
		if len(reclaimed) == 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("expected to reclaim job after lease expiry, got %d", len(reclaimed))
	}
	if reclaimed[0].ID != first[0].ID {
		t.Fatalf("reclaimed a different job")
	}
}

func TestFailRetryThenTerminal(t *testing.T) {
	conn, _ := openTestDB(t)
	// maxAttempts=2: first fail retries, second fail is terminal. A near-zero
	// backoff makes the retried job claimable again immediately (the SQLite
	// datetime modifier rounds to whole seconds, so a sub-second base is "+0
	// seconds").
	q := NewQueue(conn, WithMaxAttempts(2), WithBackoff(time.Millisecond, time.Second))
	ctx := context.Background()

	enq, err := q.Enqueue(ctx, catalog.JobKindConvertImage, "a", "pictures/a.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt 1: claim then fail -> rescheduled to pending (with backoff).
	c1, _ := q.Claim(ctx, nil, time.Minute, 1)
	if len(c1) != 1 {
		t.Fatalf("attempt-1 claim got %d", len(c1))
	}
	if err := q.Fail(ctx, enq.ID, "boom"); err != nil {
		t.Fatalf("fail 1: %v", err)
	}
	// After a retry the job is pending again (not failed).
	if got, _ := q.Get(ctx, enq.ID); got.Status != statusPending {
		t.Fatalf("after first fail want pending, got %q", got.Status)
	}

	// Attempt 2: claimable again now that backoff has elapsed.
	c2 := claimWithin(t, q, 3*time.Second)
	if len(c2) != 1 {
		t.Fatalf("attempt-2 claim got %d", len(c2))
	}
	// Attempt 2 reaches max_attempts: fail is terminal.
	if err := q.Fail(ctx, enq.ID, "boom again"); err != nil {
		t.Fatalf("fail 2: %v", err)
	}
	if got, _ := q.Get(ctx, enq.ID); got.Status != statusFailed {
		t.Fatalf("after terminal fail want failed, got %q", got.Status)
	}
}

func TestCompleteStoresData(t *testing.T) {
	conn, _ := openTestDB(t)
	q := NewQueue(conn)
	ctx := context.Background()

	enq, err := q.Enqueue(ctx, catalog.JobKindEnrichAI, "a", "pictures/a.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim(ctx, nil, time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, enq.ID, catalog.JobResult{Data: map[string]any{"caption": "a cat"}}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	got, _ := q.Get(ctx, enq.ID)
	if got.Status != statusCompleted {
		t.Fatalf("want completed, got %q", got.Status)
	}
	// Completing an unclaimed/again job is a not-found error.
	if err := q.Complete(ctx, enq.ID, catalog.JobResult{}); err == nil {
		t.Fatalf("expected error completing already-completed job")
	}
}

// TestNoDoubleClaim hammers Claim from many goroutines and asserts every job is
// claimed exactly once.
func TestNoDoubleClaim(t *testing.T) {
	conn, _ := openTestDB(t)
	q := NewQueue(conn)
	ctx := context.Background()

	const n = 50
	for i := 0; i < n; i++ {
		if _, err := q.Enqueue(ctx, catalog.JobKindConvertImage, "a", "pictures/a.jpg", nil); err != nil {
			t.Fatal(err)
		}
	}

	var (
		mu     sync.Mutex
		seen   = map[string]int{}
		total  int
		wg     sync.WaitGroup
		errBox []error
	)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				claimed, err := q.Claim(ctx, nil, time.Minute, 3)
				if err != nil {
					mu.Lock()
					errBox = append(errBox, err)
					mu.Unlock()
					return
				}
				if len(claimed) == 0 {
					return
				}
				mu.Lock()
				for _, j := range claimed {
					seen[j.ID]++
					total++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(errBox) != 0 {
		t.Fatalf("claim errors: %v", errBox)
	}
	if total != n {
		t.Fatalf("claimed %d jobs, want %d", total, n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("job %s claimed %d times (double-claim)", id, c)
		}
	}
}

// TestDurabilityAcrossRestart enqueues, closes the DB, reopens it with a fresh
// connection/queue and confirms the pending job is still there and claimable.
func TestDurabilityAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "durable.db")
	ctx := context.Background()

	conn1, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	q1 := NewQueue(conn1)
	enq, err := q1.Enqueue(ctx, catalog.JobKindTranscodeVideo, "v", "videos/a.mov", map[string]string{"crf": "20"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := conn1.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}

	// "Restart": brand-new connection and queue over the same file.
	conn2, err := db.Open(ctx, path)
	if err != nil {
		t.Fatalf("open 2: %v", err)
	}
	defer func() { _ = conn2.Close() }()
	q2 := NewQueue(conn2)

	claimed, err := q2.Claim(ctx, nil, time.Minute, 5)
	if err != nil {
		t.Fatalf("claim after restart: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != enq.ID {
		t.Fatalf("durable job lost across restart: %+v", claimed)
	}
	if claimed[0].Params["crf"] != "20" {
		t.Fatalf("params lost across restart: %+v", claimed[0].Params)
	}
}

// claimWithin polls Claim until it returns a job or the deadline passes.
func claimWithin(t *testing.T, q *Queue, d time.Duration) []catalog.Job {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		got, err := q.Claim(context.Background(), nil, time.Minute, 1)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if len(got) > 0 {
			return got
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil
}
