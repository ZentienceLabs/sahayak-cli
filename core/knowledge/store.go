package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
)

// Ext is the knowledge-pack file extension.
const Ext = ".sahayakpack"

// Store manages installed packs on disk (default ~/.sahayak/packs).
type Store struct {
	Dir string
}

// DefaultDir returns the per-user packs directory.
func DefaultDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".sahayak", "packs")
	}
	return filepath.Join(".sahayak", "packs")
}

// NewStore returns a Store rooted at dir (DefaultDir if empty).
func NewStore(dir string) *Store {
	if dir == "" {
		dir = DefaultDir()
	}
	return &Store{Dir: dir}
}

// Install validates a pack file and copies it into the store. It HARD-FAILS if the
// pack's embedding model doesn't match the embedder Sahayak will query with — a
// pack embedded by one model is meaningless to another.
func (s *Store) Install(srcPath string, e embed.Embedder) (Manifest, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()

	pack, err := ReadPack(f) // also verifies integrity + format
	if err != nil {
		return Manifest{}, err
	}
	if pack.Manifest.EmbedModelID != e.ID() {
		return Manifest{}, fmt.Errorf(
			"embed-model mismatch: pack was built with %q but Sahayak is configured for %q — install a matching pack or switch embedders",
			pack.Manifest.EmbedModelID, e.ID())
	}
	if e.Dim() != 0 && pack.Manifest.EmbedDim != e.Dim() {
		return Manifest{}, fmt.Errorf("embed-dim mismatch: pack=%d embedder=%d", pack.Manifest.EmbedDim, e.Dim())
	}

	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Manifest{}, err
	}
	dst := filepath.Join(s.Dir, packFilename(pack.Manifest))
	if err := copyFile(srcPath, dst); err != nil {
		return Manifest{}, err
	}
	return pack.Manifest, nil
}

// List returns the manifests of all installed packs.
func (s *Store) List() ([]Manifest, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Manifest
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		p, err := s.readFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue // skip corrupt packs in listings
		}
		out = append(out, p.Manifest)
	}
	return out, nil
}

// LoadAll reads every installed pack into memory (they are small per-CLI doc sets).
func (s *Store) LoadAll() ([]Pack, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var packs []Pack
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		p, err := s.readFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		packs = append(packs, p)
	}
	return packs, nil
}

// Remove deletes an installed pack by name (any version).
func (s *Store) Remove(name string) error {
	manifests, err := s.List()
	if err != nil {
		return err
	}
	removed := 0
	for _, m := range manifests {
		if m.Name == name {
			if err := os.Remove(filepath.Join(s.Dir, packFilename(m))); err == nil {
				removed++
			}
		}
	}
	if removed == 0 {
		return fmt.Errorf("no installed pack named %q", name)
	}
	return nil
}

func (s *Store) readFile(path string) (Pack, error) {
	f, err := os.Open(path)
	if err != nil {
		return Pack{}, err
	}
	defer f.Close()
	return ReadPack(f)
}

func packFilename(m Manifest) string {
	base := sanitize(m.Name)
	if m.Version != "" {
		base += "@" + sanitize(m.Version)
	}
	return base + Ext
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '-'
		}
		return r
	}, s)
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o644)
}
