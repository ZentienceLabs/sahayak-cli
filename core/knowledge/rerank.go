package knowledge

import (
	"context"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// MMRReranker reorders candidates by Maximal Marginal Relevance: it greedily picks
// the chunk that is most relevant to the query while least redundant with what it has
// already chosen. This fixes a real failure of pure top-k retrieval over runbooks —
// the top hits are often near-duplicates of one paragraph, crowding out a second,
// complementary fact the model also needs. MMR trades a little raw relevance for
// coverage, which grounds multi-part answers better.
//
// It is model-FREE: it reuses the embeddings already on each Result plus one query
// embedding, so it stays CPU-only and sovereign. A cross-encoder reranker can later
// implement the same Reranker interface for a stronger (model-based) signal.
type MMRReranker struct {
	Embedder embed.Embedder
	// Lambda in [0,1] trades relevance (1.0) against diversity (0.0). 0.7 keeps
	// relevance dominant while still suppressing duplicates.
	Lambda float64
}

// NewMMRReranker returns an MMRReranker with a sensible default lambda.
func NewMMRReranker(e embed.Embedder) *MMRReranker {
	return &MMRReranker{Embedder: e, Lambda: 0.7}
}

// Rerank applies MMR over the candidates. Candidates lacking a usable vector (e.g. a
// model-pinned-out pack) fall back to their first-stage score and are appended after
// the vector-rerankable ones, so they are never silently dropped.
func (m *MMRReranker) Rerank(ctx context.Context, query string, in []Result) ([]Result, error) {
	if len(in) <= 1 || m.Embedder == nil {
		return in, nil
	}
	qv, err := m.Embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	lambda := m.Lambda
	if lambda <= 0 || lambda > 1 {
		lambda = 0.7
	}

	// Partition: only candidates with a vector of the query's dimension can take part
	// in MMR. Others keep their RRF order and trail behind.
	type cand struct {
		res Result
		rel float64
	}
	var pool []cand
	var noVec []Result
	for _, r := range in {
		if len(r.Vector) == len(qv) && len(qv) > 0 {
			pool = append(pool, cand{res: r, rel: vector.Cosine(qv, r.Vector)})
		} else {
			noVec = append(noVec, r)
		}
	}
	if len(pool) == 0 {
		return in, nil
	}

	selected := make([]Result, 0, len(pool))
	chosen := make([]bool, len(pool))
	for range pool {
		bestIdx, bestScore := -1, -1.0e18
		for i, c := range pool {
			if chosen[i] {
				continue
			}
			// Redundancy = max similarity to anything already selected.
			maxSim := 0.0
			for _, s := range selected {
				if sim := vector.Cosine(c.res.Vector, s.Vector); sim > maxSim {
					maxSim = sim
				}
			}
			score := lambda*c.rel - (1-lambda)*maxSim
			if score > bestScore {
				bestScore, bestIdx = score, i
			}
		}
		chosen[bestIdx] = true
		selected = append(selected, pool[bestIdx].res)
	}
	return append(selected, noVec...), nil
}
