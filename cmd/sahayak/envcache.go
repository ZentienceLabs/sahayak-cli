package main

import (
	"github.com/ZentienceLabs/sahayak-cli/core/envfacts"
)

// envCache adapts *envfacts.Store to agent.EnvCache (the seed/learn/invalidate
// surface the investigate loop uses). The store itself already satisfies
// curator.Facts (Prune/Len/Summary/Save), so the same instance backs both the
// foreground agent and the background curator.
type envCache struct{ s *envfacts.Store }

// Hint renders known topology as a grounding block for the first investigate step,
// clearly flagged as cached so the model still verifies before trusting it.
func (e envCache) Hint() string {
	summary := e.s.Summary()
	if summary == "" {
		return ""
	}
	return "Known environment topology (cached from earlier runs — may be stale, verify before trusting):\n" + summary + "\n\n"
}

func (e envCache) LearnFromKubectl(args []string, stdout string) int {
	return envfacts.ExtractFromKubectl(args, stdout, e.s)
}

func (e envCache) InvalidateFromError(stderr string) int {
	return e.s.InvalidateFromError(stderr)
}
