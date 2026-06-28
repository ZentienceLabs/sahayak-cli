// Package llm defines Sahayak's model-agnostic provider interface and the
// structured Plan schema the agent loop relies on. Concrete brains (Ollama,
// embedded llama-server, …) implement Provider so the rest of the system never
// depends on a specific model or runtime.
package llm

import (
	"context"
	"encoding/json"
)

// Role identifies who authored a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single turn in a chat exchange.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is a model-agnostic request. JSONSchema, when non-nil, asks the
// provider to constrain output to that schema (Ollama structured outputs today;
// llama-server GBNF grammar later). When only JSON is non-nil/true semantics are
// needed, providers fall back to plain JSON mode.
type ChatRequest struct {
	Messages    []Message
	Temperature float64
	// JSONOnly requests that the reply be a single valid JSON object.
	JSONOnly bool
	// JSONSchema, when non-nil, constrains the reply to that JSON Schema (Ollama
	// "structured outputs"; llama-server GBNF later). This guarantees the reply
	// PARSES and has the right SHAPE (e.g. args is always an array of strings, never
	// a bare string or a missing field) — eliminating the malformed/truncated-JSON
	// failure class for weak models. It does not constrain semantics; Step.Normalized
	// still repairs content. Takes precedence over JSONOnly.
	JSONSchema json.RawMessage
	// OnToken, when set, is called with each streamed text delta as the model
	// generates. Providers that support streaming use it for live output; the full
	// reply is still returned. Nil means a normal blocking call.
	OnToken func(delta string)
}

// ChatResponse is the provider's reply plus light telemetry.
type ChatResponse struct {
	Content    string
	Model      string
	TokensIn   int
	TokensOut  int
	DurationMS int64
}

// Provider is any backend that can turn a chat request into a reply. All Sahayak
// brains (embedded, Ollama, …) satisfy this so they are swappable behind config.
type Provider interface {
	// Name returns a short identifier for logs/telemetry, e.g. "ollama".
	Name() string
	// Chat performs a single completion. Implementations must honor ctx cancellation.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// Health reports whether the backend is reachable and ready to serve.
	Health(ctx context.Context) error
}
