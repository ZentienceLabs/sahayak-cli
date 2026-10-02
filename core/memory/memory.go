// Package memory gives Sahayak persistence across turns and sessions, mirroring
// LangGraph's two tiers without the framework: short-term checkpoints (resume /
// crash-safety) and long-term, namespaced memories with semantic recall. Storage
// is local files (the spec's target is a single sahayak.db via modernc.org/sqlite;
// that swap lives behind this package). Everything stays on the host.
package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// Memory is one long-term entry: a fact, scoped to a namespace, with the embedding
// used for recall and the embedder that produced it (so we never mix dimensions).
type Memory struct {
	Namespace    string        `json:"namespace"`
	Text         string        `json:"text"`
	EmbedModelID string        `json:"embed_model_id"`
	Vector       vector.Vector `json:"vector"`
	CreatedAt    string        `json:"created_at,omitempty"`
}

// Store persists long-term memories and session checkpoints under Dir.
type Store struct {
	Dir      string
	embedder embed.Embedder

	mu     sync.Mutex
	mem    []Memory
	loaded bool
}

// DefaultDir returns the per-user memory root.
func DefaultDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".sahayak")
	}
	return ".sahayak"
}

// NewStore builds a memory store rooted at dir (DefaultDir if empty) using e for
// embeddings.
func NewStore(dir string, e embed.Embedder) *Store {
	if dir == "" {
		dir = DefaultDir()
	}
	return &Store{Dir: dir, embedder: e}
}

func (s *Store) memFile() string { return filepath.Join(s.Dir, "memories.json") }

func (s *Store) load() error {
	if s.loaded {
		return nil
	}
	b, err := os.ReadFile(s.memFile())
	if err != nil {
		if os.IsNotExist(err) {
			s.loaded = true
			return nil
		}
		return err
	}
	if err := json.Unmarshal(b, &s.mem); err != nil {
		return err
	}
	s.loaded = true
	return nil
}

func (s *Store) persist() error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.mem, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.memFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.memFile()) // atomic
}

// Remember embeds text and appends it to the given namespace.
func (s *Store) Remember(ctx context.Context, namespace, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	v, err := s.embedder.Embed(ctx, text)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	s.mem = append(s.mem, Memory{
		Namespace:    namespace,
		Text:         text,
		EmbedModelID: s.embedder.ID(),
		Vector:       v,
	})
	return s.persist()
}

// Recall returns the k memories most similar to query. namespace=="" searches all
// namespaces. Only memories embedded by the current embedder are considered.
func (s *Store) Recall(ctx context.Context, namespace, query string, k int) ([]Memory, error) {
	s.mu.Lock()
	if err := s.load(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	candidates := make([]Memory, 0, len(s.mem))
	for _, m := range s.mem {
		if m.EmbedModelID != s.embedder.ID() {
			continue
		}
		if namespace != "" && m.Namespace != namespace {
			continue
		}
		candidates = append(candidates, m)
	}
	s.mu.Unlock()

	if len(candidates) == 0 {
		return nil, nil
	}
	qv, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	type sc struct {
		m     Memory
		score float64
	}
	scored := make([]sc, len(candidates))
	for i, m := range candidates {
		scored[i] = sc{m, vector.Cosine(qv, m.Vector)}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if k > len(scored) {
		k = len(scored)
	}
	out := make([]Memory, k)
	for i := 0; i < k; i++ {
		out[i] = scored[i].m
	}
	return out, nil
}

// All returns every stored memory (for listing).
func (s *Store) All() ([]Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	out := make([]Memory, len(s.mem))
	copy(out, s.mem)
	return out, nil
}

// Forget removes memories whose text contains substr, returning the count removed.
func (s *Store) Forget(substr string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return 0, err
	}
	kept := s.mem[:0]
	removed := 0
	for _, m := range s.mem {
		if strings.Contains(m.Text, substr) {
			removed++
			continue
		}
		kept = append(kept, m)
	}
	s.mem = kept
	if removed > 0 {
		return removed, s.persist()
	}
	return 0, nil
}
