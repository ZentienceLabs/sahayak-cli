package cartridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A registry is a STATIC index file (JSON) — hostable on GitHub raw, blob storage, an
// internal server, or a local path for air-gap. It lists cartridges with where to fetch
// each and a checksum to verify it. There is no registry SERVICE: just files, so it works
// connected or mirrored offline, and is fully auditable. See CARTRIDGE-ARCHITECTURE.md.

// IndexEntry is one cartridge listed in a registry index.
type IndexEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Tool        string `json:"tool,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`                 // where to fetch the cartridge JSON
	SHA256      string `json:"sha256"`              // hex digest of the cartridge bytes (integrity)
	Signature   string `json:"signature,omitempty"` // optional detached signature (verified later)
	Registry    string `json:"-"`                   // source index this came from (filled at fetch)
}

// RegistryIndex is the on-disk/remote index format.
type RegistryIndex struct {
	Cartridges []IndexEntry `json:"cartridges"`
}

func registriesFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sahayak", "registries.json"), nil
}

// Registries returns the configured registry sources (URLs or local paths).
func Registries() ([]string, error) {
	path, err := registriesFile()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var srcs []string
	if err := json.Unmarshal(b, &srcs); err != nil {
		return nil, err
	}
	return srcs, nil
}

// AddRegistry registers a new index source (idempotent).
func AddRegistry(source string) error {
	srcs, err := Registries()
	if err != nil {
		return err
	}
	for _, s := range srcs {
		if s == source {
			return nil
		}
	}
	srcs = append(srcs, source)
	path, err := registriesFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(srcs, "", "  ")
	return os.WriteFile(path, b, 0o644)
}

// FetchRegistry loads and parses one registry index from a URL or local path.
func FetchRegistry(source string) (RegistryIndex, error) {
	data, err := fetch(source)
	if err != nil {
		return RegistryIndex{}, err
	}
	var idx RegistryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return RegistryIndex{}, fmt.Errorf("registry %s: %w", source, err)
	}
	for i := range idx.Cartridges {
		idx.Cartridges[i].Registry = source
	}
	return idx, nil
}

// allEntries fetches every configured registry and flattens their entries.
func allEntries() ([]IndexEntry, []error) {
	srcs, err := Registries()
	if err != nil {
		return nil, []error{err}
	}
	var entries []IndexEntry
	var errs []error
	for _, s := range srcs {
		idx, err := FetchRegistry(s)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		entries = append(entries, idx.Cartridges...)
	}
	return entries, errs
}

// Search returns registry entries whose name/tool/description matches query (substring,
// case-insensitive); an empty query lists everything.
func Search(query string) ([]IndexEntry, []error) {
	entries, errs := allEntries()
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entries, errs
	}
	var hits []IndexEntry
	for _, e := range entries {
		hay := strings.ToLower(e.Name + " " + e.Tool + " " + e.Description)
		if strings.Contains(hay, q) {
			hits = append(hits, e)
		}
	}
	return hits, errs
}

// Resolve finds a cartridge by name across registries (first match wins).
func Resolve(name string) (IndexEntry, error) {
	entries, _ := allEntries()
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return e, nil
		}
	}
	return IndexEntry{}, fmt.Errorf("no cartridge %q in any registry (try `cartridge registry add <url>` or `cartridge search`)", name)
}

// InstallByName resolves a name via the registries, downloads it, VERIFIES integrity
// (checksum) and AUTHENTICITY (signature), then installs it. It returns the cartridge and
// a human-readable trust note. Policy: a bad checksum or a signature that no trusted key
// verifies is REFUSED; an unsigned cartridge is allowed but flagged (or refused when
// requireSigned is set). The caller should show the cartridge's declared commands for
// review — integrity + authenticity + review are the supply-chain gate.
func InstallByName(name string, requireSigned bool) (Cartridge, string, error) {
	entry, err := Resolve(name)
	if err != nil {
		return Cartridge{}, "", err
	}
	data, err := fetch(entry.URL)
	if err != nil {
		return Cartridge{}, "", err
	}
	// Integrity.
	if entry.SHA256 != "" {
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, entry.SHA256) {
			return Cartridge{}, "", fmt.Errorf("checksum mismatch for %q: index %s, downloaded %s — refusing", name, entry.SHA256, got)
		}
	}
	// Authenticity.
	note := "unsigned (integrity verified by checksum)"
	if entry.Signature != "" {
		trusted, _ := TrustedKeys()
		signer, ok := VerifySignature(data, entry.Signature, trusted)
		if !ok {
			return Cartridge{}, "", fmt.Errorf("signature on %q is not from any trusted key — refusing (add the publisher's key with `cartridge trust add`)", name)
		}
		note = "signature verified (signer " + short(signer) + ")"
	} else if requireSigned {
		return Cartridge{}, "", fmt.Errorf("%q is unsigned and signed cartridges are required — refusing", name)
	}
	c, err := installBytes(data)
	if err != nil {
		return Cartridge{}, "", err
	}
	return c, note, nil
}

func short(b64 string) string {
	if len(b64) > 12 {
		return b64[:12] + "…"
	}
	return b64
}

// Remove deletes an installed cartridge by name. Built-in cartridges can't be removed
// (they're embedded); returns an error if the named cartridge isn't installed.
func Remove(name string) error {
	dir, err := InstallDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".json")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("cartridge %q is not installed (built-ins can't be removed)", name)
	}
	return os.Remove(path)
}
