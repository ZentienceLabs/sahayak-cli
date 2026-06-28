package cartridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFetchRegistryParsesLocalIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")
	idxJSON := `{"cartridges":[
		{"name":"k8s","version":"1.0.0","tool":"kubectl","description":"k8s ops","url":"./k8s.json","sha256":"abc"},
		{"name":"redis","tool":"redis-cli","url":"./redis.json","sha256":"def"}
	]}`
	if err := os.WriteFile(path, []byte(idxJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := FetchRegistry(path)
	if err != nil {
		t.Fatalf("FetchRegistry: %v", err)
	}
	if len(idx.Cartridges) != 2 {
		t.Fatalf("want 2 entries, got %d", len(idx.Cartridges))
	}
	if idx.Cartridges[0].Name != "k8s" || idx.Cartridges[0].SHA256 != "abc" {
		t.Errorf("entry 0 wrong: %+v", idx.Cartridges[0])
	}
	// Each entry records which registry it came from (for multi-registry display).
	if idx.Cartridges[0].Registry != path {
		t.Errorf("Registry not stamped: %q", idx.Cartridges[0].Registry)
	}
}

func TestFetchRegistryRejectsBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)
	if _, err := FetchRegistry(path); err == nil {
		t.Error("expected an error for malformed index")
	}
}
