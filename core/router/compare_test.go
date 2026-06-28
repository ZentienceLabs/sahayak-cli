package router

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// TestEmbedderCoverageComparison is a manual harness (run with -run + -v) that
// quantifies hash-vs-semantic routing coverage on deliberately DISTANT phrasings —
// wordings chosen to share few keywords with catalog.txt, so a lexical embedder
// struggles where a semantic one should not. It is skipped unless Ollama is up.
//
//	go test ./core/router -run TestEmbedderCoverageComparison -v
func TestEmbedderCoverageComparison(t *testing.T) {
	if os.Getenv("SAHAYAK_COMPARE") != "1" {
		t.Skip("manual harness: set SAHAYAK_COMPARE=1 (and have ollama + nomic-embed-text) to run")
	}
	const endpoint = "http://127.0.0.1:11434"
	if !ollamaUp(endpoint) {
		t.Skip("ollama not reachable; skipping live embedder comparison")
	}

	// Each case: a distant phrasing + the kind it SHOULD route to.
	cases := []struct {
		req  string
		want string
	}{
		{"round up the services owned by acme-web", "list"},
		{"give me the pods under acme-worker", "list"},
		{"acme-web seems unhealthy, dig into it", "logs"},
		{"track down the errors hitting acme-worker", "logs"},
		{"what release is acme-web pinned to", "image"},
		{"tell me the docker tag deployed for acme-web", "image"},
		{"did acme-web come up cleanly after deploy", "rollout"},
		{"is the acme-worker update done yet", "rollout"},
		{"kick acme-worker to pick up new config", "restart"},
		{"force a fresh start of acme-web", "restart"},
		{"make sure DEBUG_MODE is turned on for acme-web", "verifyenv"},
		{"does acme-web have FEATURE_TELEMETRY defined", "verifyenv"},
		{"anything in the configmaps mentioning telemetry", "searchcfg"},
		{"do any config entries reference authentication", "searchcfg"},
	}

	embedders := []struct {
		name string
		e    embed.Embedder
	}{
		{"hash:256", embed.NewHashEmbedder(256)},
		{"nomic-embed-text", embed.NewOllamaEmbedder(endpoint, "nomic-embed-text")},
	}

	ctx := context.Background()
	for _, em := range embedders {
		r, err := New(ctx, em.e, "")
		if err != nil {
			t.Fatalf("New(%s): %v", em.name, err)
		}
		correct, fired := 0, 0
		t.Logf("\n==================== embedder: %s (threshold %.2f) ====================", em.name, r.threshold)
		t.Logf("%-50s %-10s %-10s %7s  %s", "REQUEST", "WANT", "GOT", "SCORE", "VERDICT")
		for _, c := range cases {
			kind, score := r.nearest(ctx, t, c.req)
			_, ok, _ := r.Route(ctx, c.req)
			verdict := "miss (falls through)"
			if score >= r.threshold {
				fired++
				if kind == c.want && ok {
					correct++
					verdict = "ROUTED ✓"
				} else {
					verdict = fmt.Sprintf("WRONG → %s", kind)
				}
			}
			t.Logf("%-50s %-10s %-10s %6.1f%%  %s", trunc(c.req, 50), c.want, kind, score*100, verdict)
		}
		t.Logf("--- %s: correct=%d/%d   fired=%d/%d ---", em.name, correct, len(cases), fired, len(cases))
	}
}

// nearest returns the nearest example's kind and cosine score for a request.
func (r *Router) nearest(ctx context.Context, t *testing.T, req string) (string, float64) {
	q, err := r.embedder.Embed(ctx, req)
	if err != nil {
		t.Fatalf("embed %q: %v", req, err)
	}
	best, bestScore := example{}, -1.0
	for _, ex := range r.examples {
		if s := vector.Cosine(q, ex.vec); s > bestScore {
			bestScore, best = s, ex
		}
	}
	return best.kind, bestScore
}

func ollamaUp(endpoint string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(endpoint + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
