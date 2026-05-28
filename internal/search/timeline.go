package search

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Bucket is one day of the timeline: a calendar date and how many assets were
// captured on it.
type Bucket struct {
	Date  string `json:"date"`  // "YYYY-MM-DD"
	Count int    `json:"count"` // assets captured that day
}

// TimelineQuery filters the timeline. All fields optional; an empty query covers
// every dated asset across all libraries.
type TimelineQuery struct {
	Alias string
	From  time.Time
	To    time.Time
}

// Timeline returns per-day capture counts ordered newest day first. Assets with
// no capture time are excluded (they have no place on a timeline). date() runs
// over the stored UTC RFC3339 timestamps, which SQLite parses natively.
func (ix *Index) Timeline(ctx context.Context, q TimelineQuery) ([]Bucket, error) {
	var (
		wheres = []string{"m.captured_at IS NOT NULL", "m.captured_at <> ''"}
		args   []any
	)
	if q.Alias != "" {
		wheres = append(wheres, "a.alias = ?")
		args = append(args, q.Alias)
	}
	if !q.From.IsZero() {
		wheres = append(wheres, "m.captured_at >= ?")
		args = append(args, q.From.UTC().Format(timeLayout))
	}
	if !q.To.IsZero() {
		wheres = append(wheres, "m.captured_at <= ?")
		args = append(args, q.To.UTC().Format(timeLayout))
	}

	var b strings.Builder
	b.WriteString("SELECT date(m.captured_at) AS day, COUNT(*) AS n")
	b.WriteString(" FROM metadata m JOIN assets a ON a.id = m.asset_id")
	b.WriteString(" WHERE ")
	b.WriteString(strings.Join(wheres, " AND "))
	b.WriteString(" GROUP BY day ORDER BY day DESC")

	rows, err := ix.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("search: timeline query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	buckets := make([]Bucket, 0)
	for rows.Next() {
		var day string
		var n int
		if scanErr := rows.Scan(&day, &n); scanErr != nil {
			return nil, fmt.Errorf("search: timeline scan: %w", scanErr)
		}
		buckets = append(buckets, Bucket{Date: day, Count: n})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: timeline rows: %w", err)
	}
	return buckets, nil
}

// Cameras returns the distinct camera models present in a library (or all
// libraries when alias is ""), for the camera facet UI.
func (ix *Index) Cameras(ctx context.Context, alias string) ([]string, error) {
	if alias == "" {
		rows, err := ix.db.QueryContext(ctx,
			`SELECT DISTINCT camera_model FROM metadata WHERE camera_model <> '' ORDER BY camera_model`)
		if err != nil {
			return nil, fmt.Errorf("search: cameras: %w", err)
		}
		defer func() { _ = rows.Close() }()
		out := make([]string, 0)
		for rows.Next() {
			var c string
			if scanErr := rows.Scan(&c); scanErr != nil {
				return nil, fmt.Errorf("search: cameras scan: %w", scanErr)
			}
			out = append(out, c)
		}
		return out, rows.Err()
	}
	return ix.q.ListCameras(ctx, alias)
}
