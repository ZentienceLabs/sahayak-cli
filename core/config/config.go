// Package config resolves Sahayak's runtime settings from flags and environment.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Engine selects which brain backs the session.
type Engine string

const (
	EngineOllama   Engine = "ollama"
	EngineEmbedded Engine = "embedded"
	EngineCloud    Engine = "cloud"
)

// Config holds resolved settings for a run.
type Config struct {
	Engine          Engine `json:"engine"`
	Endpoint        string `json:"endpoint"`
	Model           string `json:"model"`
	AutoRunReadOnly bool   `json:"auto_run_readonly"`
	Embedder        string `json:"embedder"`
	CloudProvider   string `json:"cloud_provider"`
}

// Defaults returns baseline settings, overlaying JSON config and ENV vars.
func Defaults() Config {
	c := Config{
		Engine:          EngineOllama,
		Endpoint:        "http://127.0.0.1:11434",
		Model:           "qwen3:4b-instruct",
		AutoRunReadOnly: true,
		Embedder:        "hash:256",
		CloudProvider:   "anthropic",
	}

	// 1. Load from ~/.sahayak/config.json
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".sahayak", "config.json")
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &c)
		}
	}

	// 2. Environment variables override JSON
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

// Save writes the given config object to ~/.sahayak/config.json
func Save(c Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".sahayak")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), b, 0644)
}
