package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Embedded is the sovereign appliance brain: a bundled llama-server (extracted
// from the binary in a release build) running a local GGUF model on a managed
// loopback port. It implements Provider, so swapping from Ollama to embedded is
// just config — the agent loop is unchanged.
//
// Lifecycle (project.md §3.3): prefer a fixed port → fall back to ephemeral →
// publish the chosen port atomically → gate on /health → reuse a warm server →
// kill on Stop. Until the llama-server binary and GGUF weights are bundled (or
// pointed at via env for dev), Chat/Health return a clear, actionable error.
type Embedded struct {
	// ModelPath overrides model resolution; empty uses env/assets discovery.
	ModelPath string
	// PreferredPort is the fixed loopback port to try first.
	PreferredPort int
	// ContextSize is the llama-server context window.
	ContextSize int

	mu     sync.Mutex
	base   string      // http://127.0.0.1:<port> once running
	proc   *os.Process // owned child, nil if we adopted a warm server
	client *http.Client
}

// NewEmbedded constructs the appliance provider. modelPath empty means discover.
func NewEmbedded(modelPath string) *Embedded {
	return &Embedded{
		ModelPath:     modelPath,
		PreferredPort: 11923,
		ContextSize:   4096,
		client:        &http.Client{Timeout: 5 * time.Minute},
	}
}

// Name implements Provider.
func (e *Embedded) Name() string { return "embedded" }

// portFile is where the running server's port is published for warm reuse.
func (e *Embedded) portFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "sahayak", "inference.port")
}

// ensureRunning makes sure a healthy llama-server is reachable, adopting a warm
// one if present or starting a fresh one. Safe to call before every request.
func (e *Embedded) ensureRunning(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.base != "" && httpHealthy(ctx, e.base) {
		return nil
	}
	// Adopt a warm server published by a previous invocation.
	if port, err := readPortFile(e.portFile()); err == nil {
		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		if httpHealthy(ctx, base) {
			e.base = base
			return nil
		}
	}
	return e.start(ctx)
}

// start resolves assets, launches llama-server on a managed port, and waits for
// health. The caller holds e.mu.
func (e *Embedded) start(ctx context.Context) error {
	bin, err := resolveServerBinary()
	if err != nil {
		return err
	}
	model, err := resolveModel(e.ModelPath)
	if err != nil {
		return err
	}
	port, err := findFreePort(e.PreferredPort)
	if err != nil {
		return err
	}

	cmd := exec.Command(bin,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"-m", model,
		"-c", fmt.Sprintf("%d", e.ContextSize),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}

	e.proc = cmd.Process
	e.base = fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := writePortFile(e.portFile(), port); err != nil {
		// non-fatal: warm reuse just won't work across invocations
		_ = err
	}

	// A 4B model can take a few seconds to load; gate on health.
	if err := waitHealthy(ctx, e.base, 90*time.Second); err != nil {
		_ = e.stopLocked()
		return err
	}
	return nil
}

// Stop terminates a server this process owns (no-op for an adopted warm server).
func (e *Embedded) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stopLocked()
}

func (e *Embedded) stopLocked() error {
	if e.proc == nil {
		return nil
	}
	err := e.proc.Kill()
	e.proc = nil
	e.base = ""
	_ = os.Remove(e.portFile())
	return err
}

// --- OpenAI-compatible chat (llama-server /v1/chat/completions) ---

type oaiChatReq struct {
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	Temperature    float64        `json:"temperature"`
	Stream         bool           `json:"stream"`
	ResponseFormat *oaiRespFormat `json:"response_format,omitempty"`
}

type oaiRespFormat struct {
	Type       string         `json:"type"`                  // "json_object" | "json_schema"
	JSONSchema *oaiJSONSchema `json:"json_schema,omitempty"` // set when constraining to a schema
}

// oaiJSONSchema is the OpenAI structured-outputs shape llama-server understands; the
// embedded backend uses it so the SHIPPED appliance gets the same grammar-level
// constraint as the Ollama dev lane (which sends the schema via `format`). Previously
// the appliance only asked for "json_object" — valid JSON, but any shape — so it was
// strictly weaker than dev. This closes that gap at the decode boundary.
type oaiJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}

type oaiChatResp struct {
	Model   string `json:"model"`
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat implements Provider against the local llama-server.
func (e *Embedded) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if err := e.ensureRunning(ctx); err != nil {
		return ChatResponse{}, err
	}
	body := oaiChatReq{Model: "local", Messages: req.Messages, Temperature: req.Temperature}
	switch {
	case req.JSONSchema != nil:
		// Grammar-level constraint to the exact schema (mirrors ollama.go's precedence).
		body.ResponseFormat = &oaiRespFormat{
			Type:       "json_schema",
			JSONSchema: &oaiJSONSchema{Name: "sahayak", Schema: req.JSONSchema},
		}
	case req.JSONOnly:
		body.ResponseFormat = &oaiRespFormat{Type: "json_object"}
	}
	buf, _ := json.Marshal(body)

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("embedded llama-server request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("llama-server %s: %s", resp.Status, string(raw))
	}
	var or oaiChatResp
	if err := json.Unmarshal(raw, &or); err != nil {
		return ChatResponse{}, err
	}
	if or.Error != nil {
		return ChatResponse{}, fmt.Errorf("llama-server error: %s", or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("llama-server returned no choices")
	}
	return ChatResponse{
		Content:    or.Choices[0].Message.Content,
		Model:      or.Model,
		TokensIn:   or.Usage.PromptTokens,
		TokensOut:  or.Usage.CompletionTokens,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// Health implements Provider: ensures the embedded server is up and ready.
func (e *Embedded) Health(ctx context.Context) error {
	return e.ensureRunning(ctx)
}
