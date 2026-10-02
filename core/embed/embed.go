// Package embed turns text into vectors for retrieval and memory. The Embedder
// interface keeps the rest of the system independent of the embedding model. Two
// implementations ship: a deterministic, offline HashEmbedder (default — zero
// deps, always works, used for tests and air-gapped first-run) and an Ollama
// embedder (higher quality when a local embedding model is available).
//
// Whichever produces a pack's vectors is pinned by ID in the pack manifest, so a
// pack can never be queried with a mismatched embedder.
package embed

import (
	"context"

	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// Embedder converts text to vectors. ID must uniquely identify the model+config
// so packs/memories can be pinned to it.
type Embedder interface {
	// ID is the stable identifier recorded in manifests, e.g. "hash-v1:256" or
	// "ollama:nomic-embed-text".
	ID() string
	// Dim is the output dimensionality.
	Dim() int
	// Embed returns the vector for a single text.
	Embed(ctx context.Context, text string) (vector.Vector, error)
	// EmbedBatch embeds many texts (default impl loops; adapters may optimize).
	EmbedBatch(ctx context.Context, texts []string) ([]vector.Vector, error)
}
