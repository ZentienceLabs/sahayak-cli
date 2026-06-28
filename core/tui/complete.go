package tui

import "strings"

// complete.go is the (pure, testable) brain of the interactive prompt's autocomplete —
// the Claude-Code / Gemini-CLI style "/" command palette and "@" entity picker. The
// Bubble Tea prompt (prompt.go) renders it; this just maps the text-before-cursor to
// suggestions, so it can be unit-tested without a terminal.

// Suggestion is one autocomplete entry.
type Suggestion struct {
	Value string // text spliced into the input when accepted
	Label string // shown in the popup
	Desc  string // short hint
}

// Sources are the live entities the "@" picker offers — filled from the cartridge index
// and the learned env cache, so "@" surfaces real cartridges, namespaces, and workloads.
type Sources struct {
	Cartridges  []string
	Namespaces  []string
	Deployments []string
	EnvVars     []string
}

// SlashCommand is a "/"-prefixed shell command shown in the palette.
type SlashCommand struct {
	Name string // includes the leading slash, e.g. "/model"
	Desc string
}

// SlashCommands is the palette. The shell dispatches these (they are not sent to the model).
var SlashCommands = []SlashCommand{
	{"/help", "show shell help"},
	{"/model", "switch the active model"},
	{"/models", "list installed models"},
	{"/cartridge", "manage tool cartridges (list/install/…)"},
	{"/learn", "self-learning suggestions"},
	{"/knowledge", "manage knowledge packs"},
	{"/memory", "long-term memory notes"},
	{"/clear", "clear the screen"},
	{"/legacy", "toggle legacy routing for this session"},
	{"/exit", "quit the shell"},
}

// Complete returns suggestions for the text BEFORE the cursor, plus the index at which an
// accepted value should be spliced in (replacing from there to the cursor). Empty slice =
// no popup. Two triggers:
//   - the line is a "/" command still being named (no space yet) → the slash palette
//   - the token under the cursor starts with "@" → entity picker (cartridges/ns/apps/env)
func Complete(before string, src Sources) ([]Suggestion, int) {
	// Slash palette: only while typing the command name (leading slash, no space yet).
	if strings.HasPrefix(before, "/") && !strings.Contains(before, " ") {
		var out []Suggestion
		for _, c := range SlashCommands {
			if strings.HasPrefix(c.Name, before) {
				out = append(out, Suggestion{Value: c.Name + " ", Label: c.Name, Desc: c.Desc})
			}
		}
		return out, 0
	}

	// Entity picker: find the token under the cursor; if it starts with "@", complete it.
	start := strings.LastIndexAny(before, " \t") + 1
	tok := before[start:]
	if !strings.HasPrefix(tok, "@") {
		return nil, 0
	}
	partial := strings.ToLower(tok[1:])

	type ent struct{ name, kind string }
	var all []ent
	for _, c := range src.Cartridges {
		all = append(all, ent{c, "cartridge"})
	}
	for _, n := range src.Deployments {
		all = append(all, ent{n, "deployment"})
	}
	for _, n := range src.Namespaces {
		all = append(all, ent{n, "namespace"})
	}
	for _, n := range src.EnvVars {
		all = append(all, ent{n, "env var"})
	}

	seen := map[string]bool{}
	var out []Suggestion
	for _, e := range all {
		if e.name == "" || seen[e.kind+"/"+e.name] {
			continue
		}
		if partial == "" || strings.Contains(strings.ToLower(e.name), partial) {
			seen[e.kind+"/"+e.name] = true
			out = append(out, Suggestion{Value: e.name, Label: e.name, Desc: e.kind})
		}
	}
	return out, start
}
