package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEmbeddedSendsJSONSchema verifies the embedded (appliance) lane forwards a
// JSONSchema as an OpenAI json_schema response_format — i.e. the shipped binary gets
// the same grammar-level constraint as the Ollama dev lane, not a weaker json_object.
func TestEmbeddedSendsJSONSchema(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{}"}}]}`)
	}))
	defer srv.Close()

	e := NewEmbedded("")
	e.base = srv.URL // reuse the fake server; ensureRunning sees it healthy and skips spawn

	_, err := e.Chat(context.Background(), ChatRequest{
		Messages:   []Message{{Role: "user", Content: "hi"}},
		JSONOnly:   true,
		JSONSchema: ClassifySchema,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	var sent oaiChatReq
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v\n%s", err, gotBody)
	}
	if sent.ResponseFormat == nil || sent.ResponseFormat.Type != "json_schema" {
		t.Fatalf("response_format type = %+v, want json_schema", sent.ResponseFormat)
	}
	if sent.ResponseFormat.JSONSchema == nil {
		t.Fatal("json_schema payload missing")
	}
	// The schema must carry our intent enum so the decode is genuinely constrained.
	if !strings.Contains(string(sent.ResponseFormat.JSONSchema.Schema), `"verifyenv"`) {
		t.Errorf("schema not forwarded intact: %s", sent.ResponseFormat.JSONSchema.Schema)
	}
}

// TestEmbeddedJSONOnlyWithoutSchema confirms the json_object fallback still applies
// when only JSONOnly is set (no schema) — unchanged behavior.
func TestEmbeddedJSONOnlyWithoutSchema(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{}"}}]}`)
	}))
	defer srv.Close()

	e := NewEmbedded("")
	e.base = srv.URL
	if _, err := e.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		JSONOnly: true,
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var sent oaiChatReq
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sent.ResponseFormat == nil || sent.ResponseFormat.Type != "json_object" {
		t.Fatalf("want json_object fallback, got %+v", sent.ResponseFormat)
	}
}
