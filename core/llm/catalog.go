package llm

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// catalog.go is the data-driven list of hosted ("cloud") model providers and the
// models they offer. It is intentionally NOT behind the `cloud` build tag: the
// catalog is just data, so `sahayak cloud list` works on the default sovereign
// binary too (it shows what you *could* enable). The actual network adapters that
// talk to these providers live in cloud_enabled.go / anthropic.go / openai.go and
// ARE behind the tag, so no hosted-model code ships in the sovereign build.
//
// The list is embedded, and can be extended or overridden at runtime by a JSON
// file at ~/.sahayak/cloud-catalog.json (same shape) — entries with a matching
// `id` replace the built-in; new ids are appended. So adding a provider/model is
// editing JSON, not editing Go.

//go:embed catalog.json
var catalogRaw []byte

// CloudModel is one model a provider offers.
type CloudModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Context int    `json:"context,omitempty"`
	Note    string `json:"note,omitempty"`
}

// CloudProviderInfo describes a hosted provider and how to reach it.
type CloudProviderInfo struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	API            string       `json:"api"` // wire protocol: "anthropic" | "openai"
	APIKeyEnv      string       `json:"api_key_env"`
	BaseURLEnv     string       `json:"base_url_env"`
	DefaultBaseURL string       `json:"default_base_url"`
	DefaultModel   string       `json:"default_model"`
	Note           string       `json:"note,omitempty"`
	Models         []CloudModel `json:"models"`
}

// cloudAliases maps friendly names to catalog ids.
var cloudAliases = map[string]string{
	"claude": "anthropic",
	"gpt":    "openai",
	"oai":    "openai",
}

// CloudCatalog returns the provider list: the embedded defaults overlaid with any
// ~/.sahayak/cloud-catalog.json overrides (by id).
func CloudCatalog() []CloudProviderInfo {
	var base []CloudProviderInfo
	_ = json.Unmarshal(catalogRaw, &base)

	overrides := userCatalog()
	if len(overrides) == 0 {
		return base
	}
	byID := map[string]int{}
	for i, p := range base {
		byID[p.ID] = i
	}
	for _, p := range overrides {
		if i, ok := byID[p.ID]; ok {
			base[i] = p // replace built-in
		} else {
			base = append(base, p) // new provider
		}
	}
	return base
}

func userCatalog() []CloudProviderInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(home, ".sahayak", "cloud-catalog.json"))
	if err != nil {
		return nil
	}
	var c []CloudProviderInfo
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	return c
}

// CloudProviderByID resolves a provider id (or alias). Empty resolves to the
// first catalog entry (anthropic by default).
func CloudProviderByID(id string) (CloudProviderInfo, bool) {
	cat := CloudCatalog()
	if id == "" && len(cat) > 0 {
		return cat[0], true
	}
	if a, ok := cloudAliases[id]; ok {
		id = a
	}
	for _, p := range cat {
		if p.ID == id {
			return p, true
		}
	}
	return CloudProviderInfo{}, false
}

// ResolveBaseURL picks the provider's base URL: the env override if set, else the
// catalog default (trailing slash trimmed).
func (p CloudProviderInfo) ResolveBaseURL() string {
	base := p.DefaultBaseURL
	if p.BaseURLEnv != "" {
		if v := os.Getenv(p.BaseURLEnv); v != "" {
			base = v
		}
	}
	return strings.TrimRight(base, "/")
}

// APIKey reads the provider's API key from its configured env var.
func (p CloudProviderInfo) APIKey() string {
	if p.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(p.APIKeyEnv)
}
