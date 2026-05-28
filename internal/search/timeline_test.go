package search_test

import (
	"context"
	"testing"
	"time"

	"github.com/imyousuf/fs-image-manager/internal/search"
)

func TestTimelineBuckets(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)

	buckets, err := ix.Timeline(context.Background(), search.TimelineQuery{})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}

	// Dated assets: 2021-02-17 (p1, v1), 2021-02-18 (p2), 2022-06-01 (p3).
	// p4 has no capture time and must not appear.
	counts := map[string]int{}
	for _, b := range buckets {
		counts[b.Date] = b.Count
	}
	if counts["2021-02-17"] != 2 {
		t.Errorf("2021-02-17 count = %d, want 2", counts["2021-02-17"])
	}
	if counts["2021-02-18"] != 1 {
		t.Errorf("2021-02-18 count = %d, want 1", counts["2021-02-18"])
	}
	if counts["2022-06-01"] != 1 {
		t.Errorf("2022-06-01 count = %d, want 1", counts["2022-06-01"])
	}
	if len(buckets) != 3 {
		t.Fatalf("want 3 buckets, got %d: %+v", len(buckets), buckets)
	}
	// Ordered newest day first.
	if buckets[0].Date != "2022-06-01" {
		t.Errorf("first bucket = %s, want 2022-06-01", buckets[0].Date)
	}
}

func TestTimelineAliasFilter(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)

	buckets, err := ix.Timeline(context.Background(), search.TimelineQuery{Alias: "videos"})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Date != "2021-02-17" || buckets[0].Count != 1 {
		t.Fatalf("videos timeline = %+v, want one 2021-02-17 x1", buckets)
	}
}

func TestTimelineDateRange(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)

	from := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2021, 12, 31, 23, 59, 59, 0, time.UTC)
	buckets, err := ix.Timeline(context.Background(), search.TimelineQuery{From: from, To: to})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	// Only the 2021 days: 02-17 (x2) and 02-18 (x1); 2022 excluded.
	if len(buckets) != 2 {
		t.Fatalf("want 2 buckets in 2021, got %+v", buckets)
	}
}

func TestCameras(t *testing.T) {
	ix, repo, _ := newIndex(t)
	seedCorpus(t, ix, repo)

	cams, err := ix.Cameras(context.Background(), "pictures")
	if err != nil {
		t.Fatalf("cameras: %v", err)
	}
	// pictures library cameras: Canon EOS R5, NIKON D850, Sony ILCE-7M3 (v1's
	// empty camera is excluded). Sorted ascending.
	want := []string{"Canon EOS R5", "NIKON D850", "Sony ILCE-7M3"}
	if len(cams) != len(want) {
		t.Fatalf("cameras = %v, want %v", cams, want)
	}
	for i := range want {
		if cams[i] != want[i] {
			t.Fatalf("cameras[%d] = %q, want %q (full %v)", i, cams[i], want[i], cams)
		}
	}
}
