// Package vector provides the minimal, pure-Go vector math Sahayak needs for
// offline retrieval and memory recall: cosine similarity and brute-force top-K.
// Brute force is fine for the per-CLI doc packs we target (well under ~100k
// chunks); swapping in an ANN index later is a contained change.
package vector

import (
	"math"
	"sort"
)

// Vector is a dense embedding.
type Vector []float32

// Cosine returns the cosine similarity of a and b in [-1, 1]. Mismatched or zero
// vectors yield 0.
func Cosine(a, b Vector) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Normalize returns a unit-length copy of v (zero vector returned unchanged).
func Normalize(v Vector) Vector {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(n))
	out := make(Vector, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

// Scored pairs an item index with its similarity score.
type Scored struct {
	Index int
	Score float64
}

// TopK returns the indices of the k corpus vectors most similar to query, highest
// score first. k is clamped to the corpus size.
func TopK(query Vector, corpus []Vector, k int) []Scored {
	scored := make([]Scored, 0, len(corpus))
	for i, v := range corpus {
		scored = append(scored, Scored{Index: i, Score: Cosine(query, v)})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if k > len(scored) {
		k = len(scored)
	}
	return scored[:k]
}
