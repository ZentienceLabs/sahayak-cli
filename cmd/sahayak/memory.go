package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/config"
	"github.com/ZentienceLabs/sahayak-cli/core/memory"
)

// runMemory dispatches the `sahayak memory` subcommands.
func runMemory(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sahayak memory <add|list|search|forget> …")
	}
	sub, rest := args[0], args[1:]
	cfg := config.Defaults()
	store := newMemoryStore(cfg)

	switch sub {
	case "add":
		text := strings.TrimSpace(strings.Join(rest, " "))
		if text == "" {
			return fmt.Errorf("usage: sahayak memory add \"<fact to remember>\"")
		}
		if err := store.Remember(ctx, "notes", text); err != nil {
			return err
		}
		fmt.Println("remembered.")
		return nil
	case "list":
		mems, err := store.All()
		if err != nil {
			return err
		}
		if len(mems) == 0 {
			fmt.Println("no memories stored")
			return nil
		}
		for _, m := range mems {
			fmt.Printf("  [%s] %s\n", m.Namespace, collapse(m.Text))
		}
		return nil
	case "search":
		query := strings.TrimSpace(strings.Join(rest, " "))
		if query == "" {
			return fmt.Errorf("usage: sahayak memory search \"<query>\"")
		}
		mems, err := store.Recall(ctx, "", query, 5)
		if err != nil {
			return err
		}
		if len(mems) == 0 {
			fmt.Println("no relevant memories")
			return nil
		}
		for i, m := range mems {
			fmt.Printf("%d. [%s] %s\n", i+1, m.Namespace, collapse(m.Text))
		}
		return nil
	case "forget":
		substr := strings.TrimSpace(strings.Join(rest, " "))
		if substr == "" {
			return fmt.Errorf("usage: sahayak memory forget \"<substring>\"")
		}
		n, err := store.Forget(substr)
		if err != nil {
			return err
		}
		fmt.Printf("forgot %d memor%s\n", n, plural(n))
		return nil
	default:
		return fmt.Errorf("unknown memory subcommand %q", sub)
	}
}

func newMemoryStore(cfg config.Config) *memory.Store {
	return memory.NewStore("", newEmbedder(cfg))
}

// memorizer adapts *memory.Store to the agent.Memorizer interface: it recalls
// across all namespaces and records handled requests under "history".
type memorizer struct{ s *memory.Store }

func (m memorizer) Recall(ctx context.Context, query string, k int) ([]string, error) {
	// Recall ONLY user-curated facts ("notes"), never auto-saved episodic history.
	// Past investigation conclusions can be wrong or stale and must not bias new runs.
	mems, err := m.s.Recall(ctx, "notes", query, k)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(mems))
	for i, mm := range mems {
		out[i] = mm.Text
	}
	return out, nil
}

func (m memorizer) Remember(ctx context.Context, text string) error {
	return m.s.Remember(ctx, "history", text)
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
