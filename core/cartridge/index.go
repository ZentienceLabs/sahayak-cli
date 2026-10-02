package cartridge

import (
	"context"
	"fmt"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/vector"
)

// DefaultThreshold mirrors the semantic router's: below it the index declines so the
// request falls through to the model/loop rather than being forced to a wrong intent.
const DefaultThreshold = 0.45

// Index is the unified, cross-cartridge router: it embeds every installed cartridge's
// catalog phrasings into ONE space, so a request is matched to the nearest intent across
// ALL cartridges (peers — no primary/secondary). The match's cartridge+template is then
// grounded by the slot engine. This is the engine half of CARTRIDGE-ARCHITECTURE.md's
// peer model; applicability/ambiguity handling layers on top via Route's results.
type Index struct {
	embedder   embed.Embedder
	threshold  float64
	carts      map[string]Cartridge
	entries    []entry
	applicable map[string]bool // nil = all applicable; else only listed cartridges route
}

type entry struct {
	cartridge string
	intent    string
	phrase    string
	vec       vector.Vector
}

// Hit is a routing result: which cartridge/template matched, the grounded command, and
// the score. ok from Route is false when nothing cleared the threshold or no slot ground.
type Hit struct {
	Cartridge Cartridge
	Template  Template
	Command   Command
	Intent    string
	Phrase    string
	Score     float64
}

// NewIndex embeds every cartridge's catalog phrasings up front.
func NewIndex(ctx context.Context, e embed.Embedder, carts []Cartridge) (*Index, error) {
	if e == nil {
		return nil, fmt.Errorf("cartridge index: nil embedder")
	}
	ix := &Index{embedder: e, threshold: DefaultThreshold, carts: map[string]Cartridge{}}
	for _, c := range carts {
		ix.carts[c.Name] = c
		for _, ce := range c.Catalog {
			for _, ph := range ce.Phrases {
				v, err := e.Embed(ctx, ph)
				if err != nil {
					return nil, fmt.Errorf("cartridge index: embedding %q: %w", ph, err)
				}
				ix.entries = append(ix.entries, entry{cartridge: c.Name, intent: ce.Intent, phrase: ph, vec: v})
			}
		}
	}
	return ix, nil
}

// SetThreshold overrides the acceptance threshold (tuning/tests).
func (ix *Index) SetThreshold(t float64) { ix.threshold = t }

// Cartridges returns the indexed cartridges (so the caller can run applicability probes).
func (ix *Index) Cartridges() []Cartridge {
	out := make([]Cartridge, 0, len(ix.carts))
	for _, c := range ix.carts {
		out = append(out, c)
	}
	return out
}

// SetApplicable restricts routing to the named cartridges — the deterministic peer
// disambiguation prune: a tool whose applicability probe failed (e.g. systemctl absent)
// is dropped so its intents never win a match on this host. nil clears the restriction.
func (ix *Index) SetApplicable(names map[string]bool) { ix.applicable = names }

// Empty reports whether there is anything to route to.
func (ix *Index) Empty() bool { return len(ix.entries) == 0 }

// Route embeds the request, finds the nearest catalog phrase across ALL cartridges, and
// — if it clears the threshold AND the matched template grounds every required slot —
// returns the grounded command. The embedder error is surfaced so the caller can degrade
// gracefully (e.g. Ollama embedder unreachable) rather than treat it as "no match".
func (ix *Index) Route(ctx context.Context, request string) (Hit, bool, error) {
	if ix.Empty() {
		return Hit{}, false, nil
	}
	q, err := ix.embedder.Embed(ctx, request)
	if err != nil {
		return Hit{}, false, err
	}
	best, bestScore := entry{}, -1.0
	for _, e := range ix.entries {
		if ix.applicable != nil && !ix.applicable[e.cartridge] {
			continue // tool not present/relevant on this host — peer prune
		}
		if s := vector.Cosine(q, e.vec); s > bestScore {
			bestScore, best = s, e
		}
	}
	if bestScore < ix.threshold {
		return Hit{}, false, nil
	}
	c := ix.carts[best.cartridge]
	tmpl, ok := c.Find(best.intent)
	if !ok {
		return Hit{}, false, nil
	}
	cmd, ok := tmpl.Build(request)
	if !ok {
		return Hit{}, false, nil // matched by meaning, but a required slot wouldn't ground
	}
	return Hit{Cartridge: c, Template: tmpl, Command: cmd, Intent: best.intent, Phrase: best.phrase, Score: bestScore}, true, nil
}
