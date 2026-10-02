package llm

import (
	"strings"
	"testing"
)

func TestDecodeStream(t *testing.T) {
	// Ollama streams one JSON object per chunk; the last has done=true + stats.
	ndjson := `{"model":"m","message":{"role":"assistant","content":"{\"sum"},"done":false}
{"model":"m","message":{"role":"assistant","content":"mary\":\"ok\"}"},"done":false}
{"model":"m","message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":12,"eval_count":7,"total_duration":3000000}
`
	var deltas int
	resp, err := decodeStream(strings.NewReader(ndjson), func(string) { deltas++ })
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"summary":"ok"}` {
		t.Fatalf("reassembled content wrong: %q", resp.Content)
	}
	if deltas != 2 {
		t.Fatalf("expected 2 token callbacks, got %d", deltas)
	}
	if resp.TokensOut != 7 || resp.DurationMS != 3 {
		t.Fatalf("stats not captured: %+v", resp)
	}
}
