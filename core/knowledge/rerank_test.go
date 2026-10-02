package knowledge

import (
	"context"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// fixedEmbedder returns one preset vector for any query — lets us test MMR ordering
// deterministically without depending on a real embedder's geometry.
type fixedEmbedder struct {
	q   vector.Vector
	id  string
	dim int
}

func (f fixedEmbedder) ID() string { return f.id }
func (f fixedEmbedder) Dim() int   { return f.dim }
func (f fixedEmbedder) Embed(_ context.Context, _ string) (vector.Vector, error) {
	return f.q, nil
}
func (f fixedEmbedder) EmbedBatch(_ context.Context, texts []string) ([]vector.Vector, error) {
	out := make([]vector.Vector, len(texts))
	for i := range texts {
		out[i] = f.q
	}
	return out, nil
}

// TestMMRPromotesDiverseOverDuplicate: given a query equidistant to two directions,
// a near-duplicate of the top hit should be demoted BELOW an equally-relevant but
// diverse chunk. Pure top-k would keep the duplicate second; MMR must not.
func TestMMRPromotesDiverseOverDuplicate(t *testing.T) {
	q := vector.Vector{1, 1, 0}
	in := []Result{
		{Chunk: Chunk{Text: "A1"}, Score: 0.30, Vector: vector.Vector{1, 0, 0}},
		{Chunk: Chunk{Text: "A2-dup"}, Score: 0.29, Vector: vector.Vector{1, 0, 0}}, // duplicate of A1
		{Chunk: Chunk{Text: "B-diverse"}, Score: 0.28, Vector: vector.Vector{0, 1, 0}},
	}
	rr := NewMMRReranker(fixedEmbedder{q: q, id: "fixed", dim: 3})
	out, err := rr.Rerank(context.Background(), "anything", in)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{out[0].Chunk.Text, out[1].Chunk.Text, out[2].Chunk.Text}
	want := []string{"A1", "B-diverse", "A2-dup"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MMR order = %v, want %v", got, want)
		}
	}
}

// TestMMRNoVectorCandidatesTrail: candidates without a usable vector keep their order
// and follow the vector-reranked ones (never silently dropped).
func TestMMRNoVectorCandidatesTrail(t *testing.T) {
	q := vector.Vector{1, 0, 0}
	in := []Result{
		{Chunk: Chunk{Text: "withvec"}, Score: 0.5, Vector: vector.Vector{1, 0, 0}},
		{Chunk: Chunk{Text: "novec"}, Score: 0.4, Vector: nil},
	}
	rr := NewMMRReranker(fixedEmbedder{q: q, id: "fixed", dim: 3})
	out, err := rr.Rerank(context.Background(), "x", in)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Chunk.Text != "withvec" || out[1].Chunk.Text != "novec" {
		t.Fatalf("no-vector candidate not trailing: %v", out)
	}
}

// TestRetrieverModelPinWarning: querying a pack with a different embedder than it was
// built with must warn and drop that pack's vector arm (keyword still works).
func TestRetrieverModelPinWarning(t *testing.T) {
	pack, err := Build(context.Background(), "k8s", "1", "", sampleChunks(), embed.NewHashEmbedder(256))
	if err != nil {
		t.Fatal(err)
	}
	// Query with a different-dimension embedder → model-pin mismatch.
	r := NewRetriever(embed.NewHashEmbedder(128), []Pack{pack})
	if len(r.Warnings()) == 0 {
		t.Fatal("expected a model-pin warning for mismatched embedder")
	}
	for _, v := range r.vectors {
		if v != nil {
			t.Errorf("mismatched pack vector arm not dropped: %v", v)
		}
	}
}

// TestRetrieverNoWarningOnMatch: matching embedder → no warning, vectors intact.
func TestRetrieverNoWarningOnMatch(t *testing.T) {
	e := embed.NewHashEmbedder(256)
	pack, err := Build(context.Background(), "k8s", "1", "", sampleChunks(), e)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRetriever(embed.NewHashEmbedder(256), []Pack{pack})
	if len(r.Warnings()) != 0 {
		t.Fatalf("unexpected warnings on matching embedder: %v", r.Warnings())
	}
}
