package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Anthropic is a Provider backed by the Claude (Anthropic) Messages API. It is the
// "power lane": the most capable brain on offer, for users who want frontier
// reasoning during development or where the sovereignty constraint does not apply.
//
// ⚠️ NON-SOVEREIGN. This is the one provider that leaves the host: it sends the
// request (your prompts, the command output the agent loop feeds back, env facts)
// to Anthropic over the network and requires an ANTHROPIC_API_KEY. The embedded
// appliance and the air-gapped story are the Ollama/Embedded engines — not this.
// It is offered as an explicit opt-in (SAHAYAK_ENGINE=anthropic), never the default.
//
// Implemented with the standard library only (no SDK), matching ollama.go /
// embedded.go and the project's single-static-binary, minimal-dependency constraint.
type Anthropic struct {
	// BaseURL is the API root, default https://api.anthropic.com.
	BaseURL string
	// Model is the Claude model ID, e.g. "claude-opus-4-8".
	Model string
	// APIKey is the Anthropic API key (from ANTHROPIC_API_KEY).
	APIKey string

	client *http.Client
}

const (
	anthropicDefaultBase  = "https://api.anthropic.com"
	anthropicDefaultModel = "claude-opus-4-8"
	anthropicVersion      = "2023-06-01"
	anthropicMaxTokens    = 4096
)

// NewAnthropic builds the Claude provider. An empty model falls back to
// claude-opus-4-8 (the most capable widely released Claude model); the base URL
// comes from ANTHROPIC_BASE_URL when set, else the public API. The key is read
// from ANTHROPIC_API_KEY — when absent, Chat/Health return a clear, actionable
// error rather than failing cryptically at request time.
func NewAnthropic(model string) *Anthropic {
	if model == "" || !strings.HasPrefix(model, "claude") {
		model = anthropicDefaultModel
	}
	base := os.Getenv("ANTHROPIC_BASE_URL")
	if base == "" {
		base = anthropicDefaultBase
	}
	return &Anthropic{
		BaseURL: strings.TrimRight(base, "/"),
		Model:   model,
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

// Name implements Provider.
func (a *Anthropic) Name() string { return "anthropic" }

type anthropicReq struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system,omitempty"`
	Messages  []anthropicMsg `json:"messages"`
	Stream    bool           `json:"stream,omitempty"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResp struct {
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	// Error envelope (HTTP 4xx/5xx bodies carry type:"error").
	Type  string `json:"type"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Chat implements Provider.
//
// JSON shaping note: rather than the Messages API's structured-outputs surface
// (which carries schema-shape constraints and an evolving beta header), we instruct
// the model in the system prompt to emit a single JSON object — embedding the schema
// when JSONSchema is set. Frontier Claude models follow this reliably, and we strip
// any stray markdown fence on the way out. This keeps the provider robust and
// dependency-free; Step.Normalized still repairs semantics downstream.
func (a *Anthropic) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if a.APIKey == "" {
		return ChatResponse{}, fmt.Errorf("anthropic: ANTHROPIC_API_KEY is not set (this engine calls the Claude API and needs a key)")
	}

	system, msgs := a.split(req.Messages)
	if directive := jsonDirective(req); directive != "" {
		if system != "" {
			system += "\n\n"
		}
		system += directive
	}

	streaming := req.OnToken != nil
	body := anthropicReq{
		Model:     a.Model,
		MaxTokens: anthropicMaxTokens,
		System:    system,
		Messages:  msgs,
		Stream:    streaming,
	}
	// Note: temperature is intentionally omitted — it is rejected (400) on the
	// current Claude models (Opus 4.7/4.8, Fable 5). Steering is via the prompt.

	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("contact anthropic at %s: %w", a.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		if msg := anthropicErrMessage(raw); msg != "" {
			return ChatResponse{}, fmt.Errorf("anthropic returned %s: %s", resp.Status, msg)
		}
		return ChatResponse{}, fmt.Errorf("anthropic returned %s: %s", resp.Status, string(raw))
	}

	if streaming {
		return decodeAnthropicStream(resp.Body, req.OnToken)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read anthropic response: %w", err)
	}
	var ar anthropicResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		return ChatResponse{}, fmt.Errorf("decode anthropic response: %w", err)
	}
	if ar.Error != nil {
		return ChatResponse{}, fmt.Errorf("anthropic error: %s", ar.Error.Message)
	}
	if ar.StopReason == "refusal" {
		return ChatResponse{}, fmt.Errorf("anthropic declined this request (safety refusal)")
	}

	var content strings.Builder
	for _, c := range ar.Content {
		if c.Type == "text" {
			content.WriteString(c.Text)
		}
	}
	return ChatResponse{
		Content:   stripFence(content.String()),
		Model:     ar.Model,
		TokensIn:  ar.Usage.InputTokens,
		TokensOut: ar.Usage.OutputTokens,
	}, nil
}

// split pulls system messages into the top-level system field (the Messages API
// keeps system separate) and returns the remaining user/assistant turns.
func (a *Anthropic) split(in []Message) (system string, msgs []anthropicMsg) {
	var sys []string
	for _, m := range in {
		if m.Role == RoleSystem {
			sys = append(sys, m.Content)
			continue
		}
		msgs = append(msgs, anthropicMsg{Role: string(m.Role), Content: m.Content})
	}
	return strings.Join(sys, "\n\n"), msgs
}

// jsonDirective returns the system-prompt instruction that constrains output to a
// single JSON object, embedding the schema when one is supplied.
func jsonDirective(req ChatRequest) string {
	switch {
	case req.JSONSchema != nil:
		return "Respond with a single valid JSON object and nothing else — no prose, no explanation, no markdown code fences. The object must conform exactly to this JSON Schema:\n" + string(req.JSONSchema)
	case req.JSONOnly:
		return "Respond with a single valid JSON object and nothing else — no prose, no explanation, no markdown code fences."
	}
	return ""
}

// stripFence removes a surrounding ```json … ``` (or bare ``` … ```) fence so a
// model that wraps its JSON anyway still yields parseable output.
func stripFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimPrefix(t, "json")
	t = strings.TrimPrefix(t, "JSON")
	if i := strings.LastIndex(t, "```"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

func anthropicErrMessage(raw []byte) string {
	var ar anthropicResp
	if json.Unmarshal(raw, &ar) == nil && ar.Error != nil {
		return ar.Error.Message
	}
	return ""
}

// decodeAnthropicStream reads the Messages API SSE stream, invoking onToken per
// text delta and accumulating the full reply. Each event is a "data: {json}" line;
// we care about content_block_delta (text) and message_delta (usage/stop) plus any
// error event.
func decodeAnthropicStream(r io.Reader, onToken func(string)) (ChatResponse, error) {
	var content strings.Builder
	var out ChatResponse
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue // ignore non-JSON keepalives
		}
		switch ev.Type {
		case "error":
			if ev.Error != nil {
				return out, fmt.Errorf("anthropic stream error: %s", ev.Error.Message)
			}
		case "message_start":
			if ev.Message.Model != "" {
				out.Model = ev.Message.Model
			}
			out.TokensIn = ev.Usage.InputTokens
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				content.WriteString(ev.Delta.Text)
				onToken(ev.Delta.Text)
			}
		case "message_delta":
			if ev.Delta.StopReason == "refusal" {
				return out, fmt.Errorf("anthropic declined this request (safety refusal)")
			}
			if ev.Usage.OutputTokens > 0 {
				out.TokensOut = ev.Usage.OutputTokens
			}
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("read anthropic stream: %w", err)
	}
	out.Content = stripFence(content.String())
	return out, nil
}

// Health implements Provider: a one-token request confirms the key works and the
// model is reachable. (There is no unauthenticated ping; we keep it tiny.)
func (a *Anthropic) Health(ctx context.Context) error {
	if a.APIKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}
	body := anthropicReq{
		Model:     a.Model,
		MaxTokens: 1,
		Messages:  []anthropicMsg{{Role: "user", Content: "ping"}},
	}
	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("anthropic unreachable at %s: %w", a.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		if msg := anthropicErrMessage(raw); msg != "" {
			return fmt.Errorf("anthropic health check failed: %s: %s", resp.Status, msg)
		}
		return fmt.Errorf("anthropic health check failed: %s", resp.Status)
	}
	return nil
}
