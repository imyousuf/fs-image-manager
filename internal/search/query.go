package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Query describes a search request. All fields are optional; an empty Query
// matches every asset (ordered newest-captured first), so the search endpoint
// degrades to a recency feed when no terms are given.
type Query struct {
	Text   string    // free-text FTS query (matched across all FTS columns)
	From   time.Time // inclusive lower bound on capture time (zero = open)
	To     time.Time // inclusive upper bound on capture time (zero = open)
	Alias  string    // restrict to a library alias
	Camera string    // exact camera_model facet
	Person string    // person-name facet (matched in the FTS persons column)
	Cursor string    // opaque pagination cursor from a previous Result
	Limit  int       // page size; <=0 uses DefaultLimit, capped at MaxLimit
}

// Result is one page of search hits.
type Result struct {
	AssetIDs   []string // ordered asset ids for this page
	NextCursor string   // pass back as Query.Cursor for the next page ("" = last)
}

// DefaultLimit and MaxLimit bound a search page, mirroring the catalog browse
// limits so the two list endpoints behave consistently.
const (
	DefaultLimit = 200
	MaxLimit     = 1000
)

// Search runs a faceted, paginated search. Ordering is by FTS relevance when a
// text query is present, otherwise by capture time descending (newest first),
// with the asset id as a stable tiebreaker so the cursor is deterministic.
//
// Cursor model: results are over a row-numbered ordering, and the cursor encodes
// the absolute offset of the last row returned. Keyset pagination is awkward here
// because the sort key (FTS rank) is not stored; at personal-library scale an
// offset cursor over a bounded result set is correct and cheap.
func (ix *Index) Search(ctx context.Context, q Query) (Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	offset, err := decodeCursor(q.Cursor)
	if err != nil {
		return Result{}, err
	}

	sqlText, args := buildSearchSQL(q)
	// Fetch one extra row to detect whether a further page exists.
	sqlText += " LIMIT ? OFFSET ?"
	args = append(args, limit+1, offset)

	rows, err := ix.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return Result{}, fmt.Errorf("search: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return Result{}, fmt.Errorf("search: scan: %w", scanErr)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("search: rows: %w", err)
	}

	var next string
	if len(ids) > limit {
		ids = ids[:limit]
		next = encodeCursor(offset + limit)
	}
	return Result{AssetIDs: ids, NextCursor: next}, nil
}

// buildSearchSQL assembles the SELECT and its positional args from the query
// facets. It joins assets -> metadata (LEFT, so un-indexed assets still surface
// on a bare query) and assets_fts (only when a text/person term needs MATCH).
func buildSearchSQL(q Query) (string, []any) {
	var (
		joins   []string
		wheres  []string
		args    []any
		orderBy string
	)

	text := strings.TrimSpace(q.Text)
	person := strings.TrimSpace(q.Person)

	if text != "" || person != "" {
		// FTS MATCH constrains to rows whose indexed text matches; relevance
		// (bm25 via the implicit rank) orders them.
		joins = append(joins, "JOIN assets_fts f ON f.asset_id = a.id")
		wheres = append(wheres, "assets_fts MATCH ?")
		args = append(args, ftsMatchExpr(text, person))
		orderBy = "ORDER BY f.rank, a.id"
	} else {
		// No text: order by capture time (newest first), nulls last, id stable.
		orderBy = "ORDER BY m.captured_at IS NULL, m.captured_at DESC, a.id"
	}

	if q.Alias != "" {
		wheres = append(wheres, "a.alias = ?")
		args = append(args, q.Alias)
	}
	if q.Camera != "" {
		wheres = append(wheres, "m.camera_model = ?")
		args = append(args, q.Camera)
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
	b.WriteString("SELECT a.id FROM assets a")
	b.WriteString(" LEFT JOIN metadata m ON m.asset_id = a.id")
	for _, j := range joins {
		b.WriteByte(' ')
		b.WriteString(j)
	}
	if len(wheres) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(wheres, " AND "))
	}
	b.WriteByte(' ')
	b.WriteString(orderBy)
	return b.String(), args
}

// ftsMatchExpr builds the FTS5 MATCH expression. Free text is passed as a
// column-less query (matches any column); a person facet is constrained to the
// persons column. Both are combined with AND when present. User input is quoted
// as FTS5 string tokens so punctuation/operators in a filename or name cannot
// inject MATCH syntax.
func ftsMatchExpr(text, person string) string {
	var parts []string
	if text != "" {
		parts = append(parts, ftsQuote(text))
	}
	if person != "" {
		parts = append(parts, "persons : "+ftsQuote(person))
	}
	return strings.Join(parts, " AND ")
}

// ftsQuote turns arbitrary user text into a safe FTS5 query: each whitespace-
// separated token is wrapped in double quotes (with internal quotes doubled),
// making it a literal string token. Tokens are ANDed implicitly by FTS5.
func ftsQuote(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return `""`
	}
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		quoted = append(quoted, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}

// encodeCursor / decodeCursor encode an integer offset as the opaque cursor
// string. A malformed cursor is rejected rather than silently treated as 0 so a
// client bug surfaces as a 400 rather than a wrong page.
func encodeCursor(offset int) string {
	return strconv.Itoa(offset)
}

func decodeCursor(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("search: invalid cursor %q", s)
	}
	return n, nil
}
