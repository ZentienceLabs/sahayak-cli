package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// OllamaEmbedder produces embeddings via a local Ollama server's /api/embeddings
// endpoint (e.g. the nomic-embed-text model). Higher semantic quality than the
// hash embedder, still fully local. Dim is discovered from the first response.
type OllamaEmbedder struct {
	Endpoint string
	Model    string

	dim    int
	client *http.Client
}

// NewOllamaEmbedder builds an Ollama-backed embedder. Empty endpoint defaults to
// loopback; empty model defaults to nomic-embed-text.
func NewOllamaEmbedder(endpoint, model string) *OllamaEmbedder {
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbedder{Endpoint: endpoint, Model: model, client: &http.Client{Timeout: 2 * time.Minute}}
}

// ID implements Embedder.
func (o *OllamaEmbedder) ID() string { return "ollama:" + o.Model }

// Dim implements Embedder (0 until the first successful Embed).
func (o *OllamaEmbedder) Dim() int { return o.dim }

type ollamaEmbedReq struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResp struct {
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error"`
}

// Embed implements Embedder.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) (vector.Vector, error) {
	buf, _ := json.Marshal(ollamaEmbedReq{Model: o.Model, Prompt: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Endpoint+"/api/embeddings", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embeddings %s: %s", resp.Status, string(raw))
	}
	var er ollamaEmbedResp
	if err := json.Unmarshal(raw, &er); err != nil {
		return nil, err
	}
	if er.Error != "" {
		return nil, fmt.Errorf("ollama embeddings error: %s", er.Error)
	}
	if len(er.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned an empty embedding")
	}
	o.dim = len(er.Embedding)
	return vector.Normalize(vector.Vector(er.Embedding)), nil
}

// EmbedBatch implements Embedder.
func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([]vector.Vector, error) {
	out := make([]vector.Vector, len(texts))
	for i, t := range texts {
		v, err := o.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}
