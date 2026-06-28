package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicSplitExtractsSystem(t *testing.T) {
	a := &Anthropic{}
	system, msgs := a.split([]Message{
		{Role: RoleSystem, Content: "you are a devops helper"},
		{Role: RoleSystem, Content: "be terse"},
		{Role: RoleUser, Content: "list pods"},
		{Role: RoleAssistant, Content: "kubectl get pods"},
	})
	if system != "you are a devops helper\n\nbe terse" {
		t.Fatalf("system messages should be merged into the system field, got %q", system)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("only user/assistant turns belong in messages, got %+v", msgs)
	}
}

func TestJSONDirective(t *testing.T) {
	if d := jsonDirective(ChatRequest{}); d != "" {
		t.Errorf("no JSON request should yield no directive, got %q", d)
	}
	schema := json.RawMessage(`{"type":"object"}`)
	if d := jsonDirective(ChatRequest{JSONSchema: schema}); d == "" || !strings.Contains(d, `{"type":"object"}`) {
		t.Errorf("schema request should embed the schema, got %q", d)
	}
	if d := jsonDirective(ChatRequest{JSONOnly: true}); d == "" {
		t.Errorf("JSONOnly should yield a directive")
	}
}

func TestStripFence(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"```\n{\"a\":1}\n```":     `{"a":1}`,
		`{"a":1}`:                 `{"a":1}`,
		"  {\"a\":1}  ":           `{"a":1}`,
	}
	for in, want := range cases {
		if got := stripFence(in); got != want {
			t.Errorf("stripFence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewAnthropicModelFallback(t *testing.T) {
	// A non-claude model (e.g. an Ollama tag carried over) falls back to the default.
	if a := NewAnthropic("qwen3:4b-instruct"); a.Model != anthropicDefaultModel {
		t.Errorf("non-claude model should fall back to %s, got %s", anthropicDefaultModel, a.Model)
	}
	if a := NewAnthropic("claude-sonnet-4-6"); a.Model != "claude-sonnet-4-6" {
		t.Errorf("an explicit claude model should be kept, got %s", a.Model)
	}
}
