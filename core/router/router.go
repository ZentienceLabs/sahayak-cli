// Package router is the data-driven, semantic half of Sahayak's intent routing. It
// complements the deterministic regex matchers in core/playbook: where those fire
// only on phrasings their code anticipates, the router matches a request to the
// NEAREST example phrase in a catalog (catalog.txt) BY MEANING, then hands the chosen
// kind back to playbook.BuildPlan for deterministic slot extraction.
//
// The design point — the answer to the "whack-a-mole" feeling: to cover a new
// phrasing you ADD A LINE to catalog.txt (data), not a regex (code). The router
// generalizes across phrasings via embeddings, while Go still constructs and gates
// every command, so reliability is unchanged — only coverage widens.
//
// Quality scales with the embedder: the offline HashEmbedder behaves like robust
// keyword overlap; an Ollama/bge embedding model makes matching genuinely semantic
// ("is the workflow toggle on" → the search-config intent with no shared keywords).
// The architecture is identical either way, and entirely CPU-only and sovereign.
package router

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/playbook"
	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

//go:embed catalog.txt
var defaultCatalog string

// knownKinds are the playbook kinds an intent may route to. A catalog line naming
// any other kind is rejected at load time, so a typo can't silently disable an intent.
var knownKinds = map[string]bool{
	"list": true, "logs": true, "image": true, "rollout": true,
	"restart": true, "verifyenv": true, "searchcfg": true,
	"status": true, // composition kind: a status rollup (see core/playbook/composite.go)
}

// DefaultThreshold is the minimum cosine similarity for the router to accept a match.
// Below it, the router declines (returns ok=false) so the request falls through to the
// model classifier / honest fallback rather than being force-fit to a wrong intent.
// Tuned for the hash embedder's lexical overlap; a semantic embedder can run higher.
const DefaultThreshold = 0.45

// example is one catalog phrase with its precomputed embedding.
type example struct {
	intent string
	kind   string
	phrase string
	vec    vector.Vector
}

// Router matches a request to a playbook kind by semantic similarity to catalog
// examples. It is safe for concurrent use after construction (read-only).
type Router struct {
	embedder  embed.Embedder
	examples  []example
	threshold float64
}

// Match is the router's decision for a request: the chosen kind, the example it
// matched, and the score — returned alongside the grounded Plan for transparency.
type Match struct {
	Plan   playbook.Plan
	Intent string
	Phrase string
	Score  float64
}

// New builds a Router from the embedded default catalog plus any extra catalog text
// (e.g. a user file), embedding every example up front. Extra examples are appended,
// so a user can add phrasings without losing the built-ins. A zero/blank extra is fine.
func New(ctx context.Context, e embed.Embedder, extra string) (*Router, error) {
	if e == nil {
		return nil, fmt.Errorf("router: nil embedder")
	}
	parsed, err := parseCatalog(defaultCatalog)
	if err != nil {
		return nil, fmt.Errorf("router: default catalog: %w", err)
	}
	if strings.TrimSpace(extra) != "" {
		more, err := parseCatalog(extra)
		if err != nil {
			return nil, fmt.Errorf("router: extra catalog: %w", err)
		}
		parsed = append(parsed, more...)
	}
	r := &Router{embedder: e, threshold: DefaultThreshold}
	for _, p := range parsed {
		v, err := e.Embed(ctx, p.phrase)
		if err != nil {
			return nil, fmt.Errorf("router: embedding %q: %w", p.phrase, err)
		}
		r.examples = append(r.examples, example{intent: p.intent, kind: p.kind, phrase: p.phrase, vec: v})
	}
	if len(r.examples) == 0 {
		return nil, fmt.Errorf("router: catalog has no examples")
	}
	return r, nil
}

// SetThreshold overrides the acceptance threshold (mainly for tuning/tests).
func (r *Router) SetThreshold(t float64) { r.threshold = t }

// Route embeds the request, finds the nearest catalog example, and — if it clears the
// threshold AND playbook.BuildPlan can ground every slot the kind needs — returns the
// grounded Plan. Otherwise ok=false: the request is left to the model classifier or the
// honest fallback. The embedder error (e.g. Ollama unreachable) is returned so the
// caller can degrade gracefully rather than treat it as "no match".
func (r *Router) Route(ctx context.Context, request string) (Match, bool, error) {
	q, err := r.embedder.Embed(ctx, request)
	if err != nil {
		return Match{}, false, err
	}
	best := example{}
	bestScore := -1.0
	for _, ex := range r.examples {
		s := vector.Cosine(q, ex.vec)
		if s > bestScore {
			bestScore, best = s, ex
		}
	}
	if bestScore < r.threshold {
		return Match{}, false, nil
	}
	pl, ok := playbook.BuildPlan(best.kind, request)
	if !ok {
		// Matched a kind by meaning, but Go could not ground a required slot (e.g. a
		// "logs" intent with no resolvable app). Decline rather than fire half-blind.
		return Match{}, false, nil
	}
	return Match{Plan: pl, Intent: best.intent, Phrase: best.phrase, Score: bestScore}, true, nil
}

// parsed is an intent example before embedding.
type parsed struct {
	intent string
	kind   string
	phrase string
}

// parseCatalog reads the simple, dependency-free catalog format (see catalog.txt):
// "intent <name> <kind>" opens a block; "- <phrase>" lines are its examples; blanks
// and #-comments are ignored. It validates the kind and that examples have a parent.
func parseCatalog(text string) ([]parsed, error) {
	var out []parsed
	var curIntent, curKind string
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			phrase := strings.TrimSpace(strings.TrimPrefix(line, "- "))
			if curIntent == "" {
				return nil, fmt.Errorf("line %d: example before any intent: %q", n+1, line)
			}
			if phrase != "" {
				out = append(out, parsed{intent: curIntent, kind: curKind, phrase: phrase})
			}
			continue
		}
		if strings.HasPrefix(line, "intent ") {
			fields := strings.Fields(line)
			if len(fields) != 3 {
				return nil, fmt.Errorf("line %d: want `intent <name> <kind>`, got %q", n+1, line)
			}
			curIntent, curKind = fields[1], fields[2]
			if !knownKinds[curKind] {
				return nil, fmt.Errorf("line %d: unknown kind %q for intent %q", n+1, curKind, curIntent)
			}
			continue
		}
		return nil, fmt.Errorf("line %d: unrecognized line %q", n+1, line)
	}
	return out, nil
}
