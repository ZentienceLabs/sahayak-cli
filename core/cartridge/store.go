package cartridge

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallDir is where downloaded/installed cartridges live (~/.sahayak/cartridges).
// Installed cartridges are peers of the built-ins — this is the "marketplace" install
// target: drop a cartridge JSON here (by hand, or via Install) and the tool is added.
func InstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sahayak", "cartridges"), nil
}

// LoadInstalled parses every *.json cartridge in the install dir. A malformed file is
// skipped with its error collected (one bad cartridge can't block the rest). Missing dir
// is not an error (nothing installed yet).
func LoadInstalled() ([]Cartridge, []error) {
	dir, err := InstallDir()
	if err != nil {
		return nil, []error{err}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no install dir yet
	}
	var carts []Cartridge
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		c, err := Parse(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		carts = append(carts, c)
	}
	return carts, errs
}

// LoadAll returns the built-in cartridges plus every installed one. An installed
// cartridge with the same name as another overrides the earlier entry (so a downloaded
// k8s cartridge can replace the built-in).
func LoadAll() ([]Cartridge, error) {
	builtins, err := Builtins()
	if err != nil {
		return nil, err
	}
	installed, _ := LoadInstalled()
	byName := map[string]Cartridge{}
	order := []string{}
	for _, c := range append(builtins, installed...) {
		if _, seen := byName[c.Name]; !seen {
			order = append(order, c.Name)
		}
		byName[c.Name] = c
	}
	out := make([]Cartridge, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out, nil
}

// Install copies/downloads a cartridge from a local path or http(s) URL into the install
// dir, validating it first. Returns the installed cartridge's name. This is the unit a
// "marketplace" distributes — a single self-describing JSON cartridge.
func Install(src string) (string, error) {
	data, err := fetch(src)
	if err != nil {
		return "", err
	}
	c, err := installBytes(data)
	if err != nil {
		return "", err
	}
	return c.Name, nil
}

// installBytes validates cartridge bytes and writes them into the install dir, returning
// the parsed cartridge. Shared by Install (file/URL) and InstallByName (registry).
func installBytes(data []byte) (Cartridge, error) {
	c, err := Parse(data) // validate before writing
	if err != nil {
		return Cartridge{}, err
	}
	dir, err := InstallDir()
	if err != nil {
		return Cartridge{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Cartridge{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, c.Name+".json"), data, 0o644); err != nil {
		return Cartridge{}, err
	}
	return c, nil
}

func fetch(src string) ([]byte, error) {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download %s: %s", src, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(src)
}
