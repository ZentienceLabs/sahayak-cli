// Package knowledge implements Sahayak's offline RAG: installable knowledge packs
// (e.g. kubectl or az CLI docs), pre-embedded so install is a fast verify-and-copy,
// and hybrid (vector + keyword) retrieval that grounds command generation and log
// analysis. Everything is local and file-based.
//
// A .sahayakpack is a single gzip-compressed JSON container. The spec's eventual
// on-disk target is a SQLite file with sqlite-vec; that swap lives behind this
// package's Store/Pack types without touching callers.
package knowledge

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// FormatVersion is bumped on incompatible pack-layout changes.
const FormatVersion = 1

// Kind categorizes a chunk so retrieval can bias toward, say, flags vs error patterns.
type Kind string

const (
	KindFlag    Kind = "flag"
	KindExample Kind = "example"
	KindError   Kind = "error_pattern"
	KindProse   Kind = "prose"
)

// Manifest describes a pack and, critically, pins the embedding model that produced
// its vectors so it can never be queried with a mismatched embedder.
type Manifest struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Source        string `json:"source"`
	Format        int    `json:"format"`
	EmbedModelID  string `json:"embed_model_id"`
	EmbedDim      int    `json:"embed_dim"`
	ChunkCount    int    `json:"chunk_count"`
	ContentSHA256 string `json:"content_sha256"`
	CreatedAt     string `json:"created_at,omitempty"`
}

// Chunk is one retrievable unit of documentation.
type Chunk struct {
	Text      string `json:"text"`
	SourceDoc string `json:"source_doc,omitempty"`
	Section   string `json:"section,omitempty"`
	Command   string `json:"command,omitempty"` // e.g. "kubectl", "az"
	Kind      Kind   `json:"kind,omitempty"`
}

// Pack is a manifest plus aligned chunks and their vectors.
type Pack struct {
	Manifest Manifest        `json:"manifest"`
	Chunks   []Chunk         `json:"chunks"`
	Vectors  []vector.Vector `json:"vectors"`
}

// contentHash hashes the chunks+vectors (not the manifest) so integrity is
// independent of mutable metadata. Deterministic via json.Marshal of a fixed shape.
func contentHash(chunks []Chunk, vectors []vector.Vector) string {
	payload := struct {
		C []Chunk         `json:"c"`
		V []vector.Vector `json:"v"`
	}{chunks, vectors}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// WritePack serializes a pack (filling ContentSHA256/ChunkCount/Format) to w.
func WritePack(w io.Writer, p Pack) error {
	if len(p.Chunks) != len(p.Vectors) {
		return fmt.Errorf("pack invalid: %d chunks but %d vectors", len(p.Chunks), len(p.Vectors))
	}
	p.Manifest.Format = FormatVersion
	p.Manifest.ChunkCount = len(p.Chunks)
	p.Manifest.ContentSHA256 = contentHash(p.Chunks, p.Vectors)

	gz := gzip.NewWriter(w)
	defer gz.Close()
	enc := json.NewEncoder(gz)
	return enc.Encode(p)
}

// ReadPack deserializes and verifies a pack from r (format version + integrity).
func ReadPack(r io.Reader) (Pack, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Pack{}, fmt.Errorf("not a valid .sahayakpack (gzip): %w", err)
	}
	defer gz.Close()

	var p Pack
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		return Pack{}, fmt.Errorf("decode pack: %w", err)
	}
	if p.Manifest.Format > FormatVersion {
		return Pack{}, fmt.Errorf("pack format v%d is newer than supported v%d — upgrade sahayak", p.Manifest.Format, FormatVersion)
	}
	if len(p.Chunks) != len(p.Vectors) {
		return Pack{}, fmt.Errorf("pack corrupt: %d chunks vs %d vectors", len(p.Chunks), len(p.Vectors))
	}
	if got := contentHash(p.Chunks, p.Vectors); got != p.Manifest.ContentSHA256 {
		return Pack{}, fmt.Errorf("pack integrity check failed (content hash mismatch)")
	}
	return p, nil
}
