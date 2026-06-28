package embed

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// HashEmbedder is a deterministic, dependency-free embedder using the feature-
// hashing ("hashing trick") technique over word unigrams and bigrams. It needs no
// model file and works fully offline — ideal as the always-available default and
// for reproducible tests.
//
// Honest limitation: hashed bag-of-words captures lexical overlap, not deep
// semantics. It pairs well with the keyword arm of hybrid retrieval (which carries
// the literal CLI tokens). For stronger semantic recall, use the Ollama embedder
// (nomic-embed-text) or a future bge-small ONNX embedder behind this same iface.
type HashEmbedder struct {
	dim int
}

// NewHashEmbedder builds a HashEmbedder with the given dimensionality (e.g. 256).
func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = 256
	}
	return &HashEmbedder{dim: dim}
}

// ID implements Embedder.
func (h *HashEmbedder) ID() string { return fmt.Sprintf("hash-v1:%d", h.dim) }

// Dim implements Embedder.
func (h *HashEmbedder) Dim() int { return h.dim }

// Embed implements Embedder.
func (h *HashEmbedder) Embed(_ context.Context, text string) (vector.Vector, error) {
	v := make(vector.Vector, h.dim)
	toks := tokenize(text)
	for i, t := range toks {
		h.add(v, t, 1)
		if i > 0 {
			h.add(v, toks[i-1]+" "+t, 0.5) // bigram, lighter weight
		}
	}
	return vector.Normalize(v), nil
}

// EmbedBatch implements Embedder.
func (h *HashEmbedder) EmbedBatch(ctx context.Context, texts []string) ([]vector.Vector, error) {
	out := make([]vector.Vector, len(texts))
	for i, t := range texts {
		v, err := h.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// add hashes a token to a bucket (with a sign bit) and accumulates weight, which
// reduces collision bias.
func (h *HashEmbedder) add(v vector.Vector, token string, w float32) {
	hh := fnv.New32a()
	_, _ = hh.Write([]byte(token))
	sum := hh.Sum32()
	idx := int(sum % uint32(h.dim))
	sign := float32(1)
	if sum&0x80000000 != 0 {
		sign = -1
	}
	v[idx] += sign * w
}

// tokenize lowercases and splits on non-alphanumeric, keeping CLI-relevant tokens.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	})
	return fields
}

// Tokenize is exported for the keyword arm of hybrid retrieval to share tokenization.
func Tokenize(s string) []string { return tokenize(s) }
