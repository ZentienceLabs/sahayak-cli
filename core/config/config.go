// Package config resolves Sahayak's runtime settings from flags and environment.
// Phase 1 keeps it minimal (endpoint/model/engine); a YAML file via Viper arrives
// in the polish phase.
package config

import "os"

// Engine selects which brain backs the session.
type Engine string

const (
	// EngineOllama talks to a local Ollama server (Phase-1 dev default).
	EngineOllama Engine = "ollama"
	// EngineEmbedded talks to the bundled llama-server (Phase 6).
	EngineEmbedded Engine = "embedded"
	// EngineCloud talks to an online (hosted) model — the optional, NON-SOVEREIGN
	// "power lane". It leaves the host and needs an API key; it is never the default.
	// The backend is chosen by SAHAYAK_CLOUD_PROVIDER (default "anthropic"/Claude),
	// so other hosted providers can slot in without a new engine. The air-gapped
	// story stays on Ollama/Embedded.
	EngineCloud Engine = "cloud"
)

// Config holds resolved settings for a run.
type Config struct {
	Engine          Engine
	Endpoint        string
	Model           string
	AutoRunReadOnly bool
	// Embedder selects the embedding backend for knowledge/memory. Forms:
	// "hash" / "hash:256" (offline default) or "ollama:nomic-embed-text".
	Embedder string
	// CloudProvider picks the hosted backend when Engine is "cloud" (e.g.
	// "anthropic"). Default "anthropic" (Claude). Ignored for local engines.
	CloudProvider string
}

// Defaults returns baseline settings, overlaying any SAHAYAK_* environment vars.
func Defaults() Config {
	c := Config{
		Engine:   EngineOllama,
		Endpoint: "http://127.0.0.1:11434",
		// Default dev/Ollama model: qwen3:4b-instruct — Apache-2.0 and the best
		// performer in our small-model eval (cleanest commands, fewest malformed
		// args). The embedded appliance still targets IBM Granite 4.0-micro (resolved
		// separately as a bundled GGUF); this default only governs the Ollama path.
		Model:           "qwen3:4b-instruct",
		AutoRunReadOnly: true,
		Embedder:        "hash:256",
		CloudProvider:   "anthropic",
	}
	if v := os.Getenv("SAHAYAK_ENDPOINT"); v != "" {
		c.Endpoint = v
	}
	if v := os.Getenv("SAHAYAK_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("SAHAYAK_ENGINE"); v != "" {
		c.Engine = Engine(v)
	}
	if v := os.Getenv("SAHAYAK_EMBEDDER"); v != "" {
		c.Embedder = v
	}
	if v := os.Getenv("SAHAYAK_CLOUD_PROVIDER"); v != "" {
		c.CloudProvider = v
	}
	return c
}
