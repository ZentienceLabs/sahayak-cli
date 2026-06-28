package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureFormat starts a fake Ollama, runs one Chat with the given request, and
// returns the raw "format" field the adapter sent.
func captureFormat(t *testing.T, req ChatRequest) json.RawMessage {
	t.Helper()
	var got struct {
		Format json.RawMessage `json:"format"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("server could not decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"{}"},"done":true}`))
	}))
	defer srv.Close()

	o := NewOllama(srv.URL, "m")
	if _, err := o.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	return got.Format
}

func TestChatSendsJSONSchemaAsFormat(t *testing.T) {
	format := captureFormat(t, ChatRequest{
		Messages:   []Message{{Role: RoleUser, Content: "hi"}},
		JSONOnly:   true,
		JSONSchema: NextActionSchema,
	})
	// The schema must be forwarded verbatim as the format (not the literal "json").
	var schema map[string]any
	if err := json.Unmarshal(format, &schema); err != nil {
		t.Fatalf("format was not a JSON schema object: %q", format)
	}
	if schema["type"] != "object" {
		t.Errorf("format schema type = %v, want object", schema["type"])
	}
	if _, ok := schema["properties"].(map[string]any)["done"]; !ok {
		t.Errorf("format schema missing expected 'done' property: %q", format)
	}
}

func TestChatFallsBackToJSONMode(t *testing.T) {
	// With JSONOnly but no schema, format must be the literal string "json".
	format := captureFormat(t, ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		JSONOnly: true,
	})
	if string(format) != `"json"` {
		t.Errorf("format = %s, want \"json\"", format)
	}
}

func TestChatNoFormatWhenUnconstrained(t *testing.T) {
	format := captureFormat(t, ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if len(format) != 0 {
		t.Errorf("format = %s, want empty (omitted)", format)
	}
}
