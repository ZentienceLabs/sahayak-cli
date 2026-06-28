package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/cartridge"
	"github.com/ZentienceLabs/sahayak-cli/core/exec"
	"github.com/ZentienceLabs/sahayak-cli/core/learn"
)

// learnerAdapter lets the agent's narrow Learner interface write to a learn.Store.
type learnerAdapter struct{ s *learn.Store }

func (l learnerAdapter) Record(kind, request, command string, args []string, cartridge, intent string, success bool) {
	_ = l.s.Record(learn.Event{
		Kind: kind, Request: request, Command: command, Args: args,
		Cartridge: cartridge, Intent: intent, Success: success,
	})
}

// runLearn handles `sahayak learn {suggest|forget}` — the self-learning review surface.
// Learning OBSERVES deterministically and SUGGESTS; promotion to a cartridge is the human
// editing/installing one (the static base is never auto-mutated).
func runLearn(_ context.Context, args []string) error {
	sub := "suggest"
	if len(args) > 0 {
		sub = args[0]
	}
	store := learn.NewStore("")
	switch sub {
	case "suggest", "list", "show":
		events, err := store.Events()
		if err != nil {
			return err
		}
		sugs := learn.Suggest(events)
		fmt.Printf("Learned from %d observation(s):\n", len(events))
		if len(sugs) == 0 {
			fmt.Println("  no suggestions yet — keep using Sahayak; repeated patterns surface here.")
			return nil
		}
		for _, s := range sugs {
			tag := map[string]string{"promote-template": "✚", "fix-template": "⚠", "cover-gap": "○"}[s.Kind]
			fmt.Printf("  %s [%s] %s\n     %s\n", tag, s.Kind, s.Title, s.Detail)
		}
		fmt.Println("\nReview these, then add a phrasing/template to a cartridge (sahayak cartridge install …).")
		return nil
	case "promote":
		return learnPromote(args[1:], store)
	case "forget", "clear", "reset":
		if err := store.Clear(); err != nil {
			return err
		}
		fmt.Println("cleared the learning log.")
		return nil
	default:
		return fmt.Errorf("unknown learn subcommand %q (suggest | promote | forget)", sub)
	}
}

// learnPromote turns a learned command into a routable command template in a dynamic
// OVERLAY cartridge — the human-gated promotion. The operator supplies the intent and the
// natural-language phrasing(s) that should trigger it (decisions a model shouldn't make);
// the command defaults to the most frequently-succeeded ad-hoc command, and its risk tier
// is classified deterministically. The shipped/static cartridges are never modified.
func learnPromote(args []string, store *learn.Store) error {
	fs := flag.NewFlagSet("learn promote", flag.ContinueOnError)
	intent := fs.String("intent", "", "intent name for the new template (required)")
	phrase := fs.String("phrase", "", "comma-separated trigger phrasings (required), e.g. \"list namespaces,show all namespaces\"")
	overlay := fs.String("cartridge", "learned", "overlay cartridge to write into")
	command := fs.String("command", "", "command to capture (default: the most-succeeded ad-hoc command)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *intent == "" || *phrase == "" {
		return fmt.Errorf("usage: sahayak learn promote --intent <name> --phrase \"a,b\" [--cartridge learned] [--command \"...\"]")
	}

	var cmd string
	var cargs []string
	if *command != "" {
		fields := strings.Fields(*command)
		if len(fields) == 0 {
			return fmt.Errorf("empty --command")
		}
		cmd, cargs = fields[0], fields[1:]
	} else {
		events, err := store.Events()
		if err != nil {
			return err
		}
		c, a, ok := learn.TopAdhocCommand(events)
		if !ok {
			return fmt.Errorf("no learned ad-hoc command to promote — run some via `!` first, or pass --command")
		}
		cmd, cargs = c, a
	}

	risk := exec.Classify(cmd, cargs).String()
	var phrases []string
	for _, p := range strings.Split(*phrase, ",") {
		if p = strings.TrimSpace(p); p != "" {
			phrases = append(phrases, p)
		}
	}
	t := cartridge.Template{
		Intent:    *intent,
		Command:   cmd,
		Args:      cargs,
		Risk:      risk,
		Processor: "raw",
		Shape:     "simple",
	}
	if err := cartridge.PromoteToOverlay(*overlay, t, phrases); err != nil {
		return err
	}
	fmt.Printf("promoted to cartridge %q: intent %q → `%s %s` (%s)\n", *overlay, *intent, cmd, strings.Join(cargs, " "), risk)
	fmt.Printf("triggers: %s\n", strings.Join(phrases, " / "))
	return nil
}
