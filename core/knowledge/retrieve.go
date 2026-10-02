package knowledge

import (
	"context"
	"fmt"
	"sort"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// Result is a retrieved chunk with its fused relevance score and origin pack.
type Result struct {
	Chunk  Chunk
	Pack   string
	Score  float64
	Vector vector.Vector // the chunk's stored embedding (for reranking); may be nil
}

// Reranker reorders an already-retrieved candidate set for a query. It is the seam
// for a stronger relevance signal than first-stage RRF: the default MMRReranker is
// model-free (diversity over the existing embeddings), and a cross-encoder reranker
// (ONNX/LLM) can drop in later behind this same interface — the nomic-style "swap a
// component, keep the contract" move, applied to retrieval quality.
type Reranker interface {
	Rerank(ctx context.Context, query string, in []Result) ([]Result, error)
}

// Retriever runs hybrid retrieval over a set of packs: a vector arm (semantic) and
// a keyword arm (literal tokens — essential for CLI flags/error codes), fused with
// Reciprocal Rank Fusion. RRF needs no score normalization and rewards chunks both
// arms agree on.
type Retriever struct {
	embedder embed.Embedder
	chunks   []Chunk
	packs    []string        // parallel to chunks: pack name per chunk
	vectors  []vector.Vector // parallel to chunks
	tokens   [][]string      // parallel to chunks: precomputed keyword tokens
	warnings []string        // model-pin mismatches found at construction

	// Reranker, when set, reorders the candidate pool before the top-k cut. nil keeps
	// the pure first-stage RRF behavior (library default; the CLI wires MMR in).
	Reranker Reranker
}

// NewRetriever flattens packs into a single searchable corpus. It also model-pins:
// a pack whose embeddings were built with a DIFFERENT embedder than the one querying
// it cannot be searched semantically (cosine across mismatched models/dims is
// meaningless — it silently returns 0), so such a pack is recorded as a warning and
// its vector arm is dropped (it still contributes via the keyword arm). Surface
// Warnings() so the operator knows to rebuild the pack rather than trust a degraded
// semantic search.
func NewRetriever(e embed.Embedder, packs []Pack) *Retriever {
	r := &Retriever{embedder: e}
	for _, p := range packs {
		mismatch := p.Manifest.EmbedModelID != "" && e != nil &&
			(p.Manifest.EmbedModelID != e.ID() || p.Manifest.EmbedDim != e.Dim())
		if mismatch {
			r.warnings = append(r.warnings, fmt.Sprintf(
				"pack %q was built with embedder %s/%dd but you are querying with %s/%dd — "+
					"semantic search disabled for it (keyword only); rebuild it to restore",
				p.Manifest.Name, p.Manifest.EmbedModelID, p.Manifest.EmbedDim, e.ID(), e.Dim()))
		}
		for i, c := range p.Chunks {
			r.chunks = append(r.chunks, c)
			r.packs = append(r.packs, p.Manifest.Name)
			if mismatch {
				r.vectors = append(r.vectors, nil) // drop the unusable vector arm for this chunk
			} else {
				r.vectors = append(r.vectors, p.Vectors[i])
			}
			r.tokens = append(r.tokens, embed.Tokenize(embedText(c)))
		}
	}
	return r
}

// Warnings returns any model-pin mismatches found at construction (empty if all packs
// match the query embedder).
func (r *Retriever) Warnings() []string { return r.warnings }

// Empty reports whether there is anything to search.
func (r *Retriever) Empty() bool { return len(r.chunks) == 0 }

// rrfK is the standard Reciprocal Rank Fusion constant.
const rrfK = 60

// Search returns the top-k chunks for query using hybrid vector+keyword RRF.
func (r *Retriever) Search(ctx context.Context, query string, k int) ([]Result, error) {
	if r.Empty() {
		return nil, nil
	}

	// Vector arm.
	qv, err := r.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	vecRanked := vector.TopK(qv, r.vectors, len(r.vectors))

	// Keyword arm (token-overlap scoring — a light BM25-style lexical match).
	kwRanked := r.keywordRank(query)

	// Reciprocal Rank Fusion across the two arms.
	fused := map[int]float64{}
	for rank, s := range vecRanked {
		if s.Score <= 0 {
			continue
		}
		fused[s.Index] += 1.0 / float64(rrfK+rank+1)
	}
	for rank, idx := range kwRanked {
		fused[idx] += 1.0 / float64(rrfK+rank+1)
	}

	results := make([]Result, 0, len(fused))
	for idx, score := range fused {
		results = append(results, Result{Chunk: r.chunks[idx], Pack: r.packs[idx], Score: score, Vector: r.vectors[idx]})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	// Second stage: rerank a candidate POOL (wider than k) then cut to k, so the
	// reranker can promote a chunk RRF ranked just outside the top-k. Without a
	// reranker this is a no-op beyond the trim.
	pool := k
	if r.Reranker != nil {
		if p := k * 3; p > pool {
			pool = p
		}
	}
	if pool < len(results) {
		results = results[:pool]
	}
	if r.Reranker != nil {
		reranked, err := r.Reranker.Rerank(ctx, query, results)
		if err != nil {
			return nil, err
		}
		results = reranked
	}
	if k < len(results) {
		results = results[:k]
	}
	return results, nil
}

// keywordRank returns chunk indices ordered by query-token overlap, best first.
// Only chunks sharing at least one token are included.
func (r *Retriever) keywordRank(query string) []int {
	qtok := map[string]bool{}
	for _, t := range embed.Tokenize(query) {
		qtok[t] = true
	}
	type sc struct {
		idx   int
		score int
	}
	var scored []sc
	for i, toks := range r.tokens {
		overlap := 0
		for _, t := range toks {
			if qtok[t] {
				overlap++
			}
		}
		if overlap > 0 {
			scored = append(scored, sc{i, overlap})
		}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	out := make([]int, len(scored))
	for i, s := range scored {
		out[i] = s.idx
	}
	return out
}
