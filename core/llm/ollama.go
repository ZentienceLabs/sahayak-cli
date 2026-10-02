package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ollama is a Provider backed by a local Ollama server (the dev/GPU-upgrade
// brain). It speaks the /api/chat endpoint in non-streaming mode. Everything
// stays on the host: the default endpoint is loopback.
type Ollama struct {
	// Endpoint is the base URL, e.g. "http://127.0.0.1:11434".
	Endpoint string
	// Model is the Ollama model tag, e.g. "granite4:micro" or "qwen2.5-coder:7b".
	Model string

	client *http.Client
}

// NewOllama builds an Ollama provider. Empty endpoint/model fall back to the
// loopback default and an empty model (caller should set one).
func NewOllama(endpoint, model string) *Ollama {
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	return &Ollama{
		Endpoint: endpoint,
		Model:    model,
		client:   &http.Client{Timeout: 5 * time.Minute},
	}
}

// Name implements Provider.
func (o *Ollama) Name() string { return "ollama" }

type ollamaChatReq struct {
	Model    string          `json:"model"`
	Messages []Message       `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format,omitempty"`
	Options  ollamaOptions   `json:"options"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
}

type ollamaChatResp struct {
	Model           string  `json:"model"`
	Message         Message `json:"message"`
	Done            bool    `json:"done"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	TotalDuration   int64   `json:"total_duration"` // nanoseconds
	Error           string  `json:"error"`
}

// Chat implements Provider.
func (o *Ollama) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	streaming := req.OnToken != nil
	body := ollamaChatReq{
		Model:    o.Model,
		Messages: req.Messages,
		Stream:   streaming,
		Options:  ollamaOptions{Temperature: req.Temperature},
	}
	switch {
	case req.JSONSchema != nil:
		body.Format = req.JSONSchema // structured outputs: constrain to this schema
	case req.JSONOnly:
		body.Format = json.RawMessage(`"json"`)
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.Endpoint+"/api/chat", bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("contact ollama at %s: %w", o.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("ollama returned %s: %s", resp.Status, string(raw))
	}
	if streaming {
		return decodeStream(resp.Body, req.OnToken)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read ollama response: %w", err)
	}
	var oresp ollamaChatResp
	if err := json.Unmarshal(raw, &oresp); err != nil {
		return ChatResponse{}, fmt.Errorf("decode ollama response: %w", err)
	}
	if oresp.Error != "" {
		return ChatResponse{}, fmt.Errorf("ollama error: %s", oresp.Error)
	}

	return ChatResponse{
		Content:    oresp.Message.Content,
		Model:      oresp.Model,
		TokensIn:   oresp.PromptEvalCount,
		TokensOut:  oresp.EvalCount,
		DurationMS: oresp.TotalDuration / 1_000_000,
	}, nil
}

// decodeStream reads Ollama's NDJSON stream, invoking onToken per delta and
// accumulating the full reply.
func decodeStream(r io.Reader, onToken func(string)) (ChatResponse, error) {
	dec := json.NewDecoder(r)
	var content strings.Builder
	var out ChatResponse
	for {
		var chunk ollamaChatResp
		if err := dec.Decode(&chunk); err == io.EOF {
			break
		} else if err != nil {
			return out, fmt.Errorf("decode ollama stream: %w", err)
		}
		if chunk.Error != "" {
			return out, fmt.Errorf("ollama error: %s", chunk.Error)
		}
		if chunk.Message.Content != "" {
			content.WriteString(chunk.Message.Content)
			onToken(chunk.Message.Content)
		}
		if chunk.Done {
			out.Model = chunk.Model
			out.TokensIn = chunk.PromptEvalCount
			out.TokensOut = chunk.EvalCount
			out.DurationMS = chunk.TotalDuration / 1_000_000
		}
	}
	out.Content = content.String()
	return out, nil
}

// Health implements Provider: a reachable Ollama answers GET / with 200.
func (o *Ollama) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Endpoint+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := o.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama unreachable at %s: %w", o.Endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health check failed: %s", resp.Status)
	}
	return nil
}
