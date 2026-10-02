package vector

import (
	"math"
	"testing"
)

func TestCosine(t *testing.T) {
	a := Vector{1, 0, 0}
	if got := Cosine(a, Vector{1, 0, 0}); math.Abs(got-1) > 1e-9 {
		t.Errorf("identical = %v, want 1", got)
	}
	if got := Cosine(a, Vector{0, 1, 0}); math.Abs(got) > 1e-9 {
		t.Errorf("orthogonal = %v, want 0", got)
	}
	if got := Cosine(a, Vector{-1, 0, 0}); math.Abs(got+1) > 1e-9 {
		t.Errorf("opposite = %v, want -1", got)
	}
	if got := Cosine(a, Vector{1, 0}); got != 0 {
		t.Errorf("mismatched dims should be 0, got %v", got)
	}
}

func TestTopK(t *testing.T) {
	q := Vector{1, 0}
	corpus := []Vector{{0, 1}, {1, 0}, {0.9, 0.1}}
	hits := TopK(q, corpus, 2)
	if len(hits) != 2 || hits[0].Index != 1 {
		t.Fatalf("expected best=index1, got %+v", hits)
	}
	if hits[0].Score < hits[1].Score {
		t.Errorf("results not sorted: %+v", hits)
	}
}

func TestNormalize(t *testing.T) {
	v := Normalize(Vector{3, 4})
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(n)-1) > 1e-6 {
		t.Errorf("not unit length: %v", v)
	}
}
