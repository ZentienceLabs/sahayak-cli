// Package curator is Sahayak's always-on background knowledge builder. It runs for
// the life of a command on the gate's BACKGROUND lane, so every model call it makes
// yields to the operator's foreground work (see core/llm.Gate). Its real idle
// window is human-thinking time — while you read output or decide on an approval
// the CPU is free, and the curator uses exactly that to consolidate what Sahayak
// has learned about your environment.
//
// Two jobs, in priority order:
//  1. Deterministic maintenance (free, no model): prune expired environment facts
//     and persist the cache.
//  2. Optional distillation (gated background model call): turn the deterministic
//     fact cache into a short, human-readable topology note. This only ever rephrases
//     already-verified facts, and it checks the gate before every call so it never
//     makes the foreground wait.
package curator

import (
	"context"
	"time"

	"github.com/ZentienceLabs/sahayak-cli/core/llm"
)

// Facts is the slice of the environment-fact store the curator consumes.
type Facts interface {
	Prune() int      // drop expired facts; returns count removed
	Len() int        // number of fresh facts
	Summary() string // compact text rendering of fresh facts
	Save() error     // persist if dirty
}

// NoteWriter persists a distilled topology note. The memory store implements it.
// The curator writes to a dedicated namespace, never the operator's curated notes.
type NoteWriter interface {
	Remember(ctx context.Context, namespace, text string) error
}

// TopologyNamespace is where distilled notes are written — kept separate from the
// user-curated "notes" namespace so machine-derived summaries never poison recall.
const TopologyNamespace = "topology"

// Curator coordinates background knowledge work. Construct with New and call Run in
// a goroutine; cancel its context to stop (Run does a final Save on the way out).
type Curator struct {
	gate     *llm.Gate    // to check ForegroundWaiting before model work
	provider llm.Provider // background-wrapped provider (yields to foreground)
	facts    Facts
	notes    NoteWriter // optional; nil disables distillation persistence

	// Interval between maintenance ticks. Small, because ticks are cheap and we
	// want to catch idle windows like approval pauses.
	Interval time.Duration
	// Distill enables the optional background model distillation pass.
	Distill bool

	lastSummary string // de-dupe: skip distillation when facts haven't changed
}

// New builds a curator. provider should be the gate's Background(...) wrapper.
func New(gate *llm.Gate, provider llm.Provider, facts Facts, notes NoteWriter) *Curator {
	return &Curator{
		gate:     gate,
		provider: provider,
		facts:    facts,
		notes:    notes,
		Interval: 3 * time.Second,
		Distill:  true,
	}
}

// Run loops until ctx is cancelled, doing one maintenance step per tick. It is
// safe to run with no facts (it simply idles) and never returns an error: a
// background helper must not be able to break the foreground command.
func (c *Curator) Run(ctx context.Context) {
	t := time.NewTicker(c.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = c.facts.Save() // best-effort flush of anything learned this run
			return
		case <-t.C:
			c.step(ctx)
		}
	}
}

// step performs one unit of background work. Deterministic maintenance always
// runs; the model-backed distillation runs only when enabled, when there is
// something new to summarize, and when no foreground request is waiting.
func (c *Curator) step(ctx context.Context) {
	// 1. Free, deterministic maintenance.
	c.facts.Prune()
	_ = c.facts.Save()

	// 2. Optional, gated distillation — but never elbow the operator aside.
	if !c.Distill || c.provider == nil || c.notes == nil {
		return
	}
	if c.gate != nil && c.gate.ForegroundWaiting() {
		return // a foreground request wants the model; stay out of its way
	}
	summary := c.facts.Summary()
	if summary == "" || summary == c.lastSummary || c.facts.Len() == 0 {
		return // nothing new to consolidate
	}
	note, err := c.distill(ctx, summary)
	if err != nil || note == "" {
		return // background work fails silently; try again next tick
	}
	if err := c.notes.Remember(ctx, TopologyNamespace, note); err == nil {
		c.lastSummary = summary
	}
}

// distill asks the model to compress the verified fact summary into one concise,
// operator-friendly note. The provider is the background-gated one, so this call
// waits its turn behind any foreground inference automatically.
func (c *Curator) distill(ctx context.Context, summary string) (string, error) {
	resp, err := c.provider.Chat(ctx, llm.ChatRequest{
		Temperature: 0.1,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: distillPrompt},
			{Role: llm.RoleUser, Content: "Known environment facts (already verified):\n" + summary},
		},
	})
	if err != nil {
		return "", err
	}
	return oneLine(resp.Content), nil
}

const distillPrompt = `You maintain a DevOps operator's environment notes. You are given a list of ALREADY-VERIFIED facts about their cluster (namespaces, deployments, services). Write ONE short sentence (max 30 words) capturing the durable shape of their environment that would help recall it later. Only restate the given facts — never invent names, counts, or status. No preamble, just the sentence.`

// oneLine collapses whitespace/newlines into a single trimmed line.
func oneLine(s string) string {
	fields := make([]rune, 0, len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		fields = append(fields, r)
	}
	out := string(fields)
	for len(out) > 0 && out[0] == ' ' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}
