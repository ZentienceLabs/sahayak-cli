package cartridge

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// PromoteToOverlay merges a command template and its trigger phrases into a dynamic-overlay
// cartridge (created if absent) in the install dir — so a learned, human-approved command
// becomes routable next run. This is the human-gated promotion target from the self-learning
// loop: the operator supplies the intent + phrasing (the decisions a model shouldn't make);
// Go writes the data. The static base (built-in/shipped cartridges) is never touched — the
// overlay is a separate installed cartridge. Replaces an existing template/catalog entry of
// the same intent so re-promoting updates in place.
func PromoteToOverlay(overlay string, t Template, phrases []string) error {
	dir, err := InstallDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, overlay+".json")

	c := Cartridge{Name: overlay}
	if b, err := os.ReadFile(path); err == nil {
		if existing, perr := Parse(b); perr == nil {
			c = existing
		}
	}

	replaced := false
	for i := range c.Templates {
		if c.Templates[i].Intent == t.Intent {
			c.Templates[i] = t
			replaced = true
		}
	}
	if !replaced {
		c.Templates = append(c.Templates, t)
	}

	ci := -1
	for i := range c.Catalog {
		if c.Catalog[i].Intent == t.Intent {
			ci = i
		}
	}
	if ci >= 0 {
		c.Catalog[ci].Phrases = phrases
	} else {
		c.Catalog = append(c.Catalog, CatalogEntry{Intent: t.Intent, Phrases: phrases})
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if _, err := Parse(data); err != nil { // validate before writing
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
