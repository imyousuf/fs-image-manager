package search

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/imyousuf/fs-image-manager/internal/db/store"
)

// The embedding store keeps one semantic-search vector per asset. ai-people
// computes vectors on the worker and calls UpsertEmbedding; SearchSimilar does a
// brute-force cosine scan over them. Pure Go, no C extension and no vector
// SQLite extension -- correct and fast enough at personal-library scale, and the
// single place to swap in an ANN index later (see docs/TECH_SPEC.md sec.8).

// Match is one semantic-search hit: an asset and its cosine similarity to the
// query vector (1.0 = identical direction, 0 = orthogonal).
type Match struct {
	AssetID string
	Score   float64
}

// UpsertEmbedding stores (or replaces) the embedding vector for an asset. The
// vector is packed little-endian float32; an empty vector is rejected so a
// zero-dim row never pollutes the search.
func (ix *Index) UpsertEmbedding(ctx context.Context, assetID string, vec []float32) error {
	if len(vec) == 0 {
		return fmt.Errorf("search: empty embedding for %s", assetID)
	}
	return ix.q.UpsertEmbedding(ctx, store.UpsertEmbeddingParams{
		AssetID: assetID,
		Dim:     int64(len(vec)),
		Vec:     packFloats(vec),
	})
}

// GetEmbedding returns the stored vector for an asset, or ok=false if none.
func (ix *Index) GetEmbedding(ctx context.Context, assetID string) ([]float32, bool, error) {
	row, err := ix.q.GetEmbedding(ctx, assetID)
	if err != nil {
		if isNoRows(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("search: get embedding %s: %w", assetID, err)
	}
	vec, err := unpackFloats(row.Vec, int(row.Dim))
	if err != nil {
		return nil, false, fmt.Errorf("search: decode embedding %s: %w", assetID, err)
	}
	return vec, true, nil
}

// SearchSimilar returns the top-k assets whose stored embedding is most cosine-
// similar to query, ordered by descending score. Rows whose dimensionality
// differs from the query are skipped (an incompatible model) rather than erroring
// the whole search. k <= 0 returns no results.
func (ix *Index) SearchSimilar(ctx context.Context, query []float32, k int) ([]Match, error) {
	if k <= 0 || len(query) == 0 {
		return nil, nil
	}
	rows, err := ix.q.ListEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("search: list embeddings: %w", err)
	}

	qnorm := norm(query)
	if qnorm == 0 {
		return nil, nil
	}

	matches := make([]Match, 0, len(rows))
	for _, row := range rows {
		if int(row.Dim) != len(query) {
			continue // different embedding model; not comparable
		}
		vec, derr := unpackFloats(row.Vec, int(row.Dim))
		if derr != nil {
			continue // skip a corrupt row rather than failing the query
		}
		vn := norm(vec)
		if vn == 0 {
			continue
		}
		score := dot(query, vec) / (qnorm * vn)
		matches = append(matches, Match{AssetID: row.AssetID, Score: score})
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].AssetID < matches[j].AssetID // stable tiebreak
	})
	if len(matches) > k {
		matches = matches[:k]
	}
	return matches, nil
}

// packFloats serializes a []float32 to a little-endian byte slice.
func packFloats(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// unpackFloats deserializes a little-endian byte slice into dim float32s. It
// verifies the byte length matches dim*4 so a truncated/malformed blob is caught.
func unpackFloats(b []byte, dim int) ([]float32, error) {
	if dim < 0 || len(b) != dim*4 {
		return nil, fmt.Errorf("embedding: blob len %d != dim %d*4", len(b), dim)
	}
	out := make([]float32, dim)
	for i := 0; i < dim; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// dot is the dot product of two equal-length vectors.
func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// norm is the Euclidean length of a vector.
func norm(v []float32) float64 {
	var s float64
	for _, f := range v {
		s += float64(f) * float64(f)
	}
	return math.Sqrt(s)
}
