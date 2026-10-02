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

// SlashArg is a subcommand/argument of a slash command, shown when completing args.
type SlashArg struct {
	Name string
	Desc string
}

// slashArgs maps a command path (no leading slash) to its first-level subcommands, so
// "/cartridge ins" → install and "/cartridge registry " → add/list. Mirrors the real CLI
// subcommands the shell dispatches.
var slashArgs = map[string][]SlashArg{
	"cartridge": {
		{"list", "show installed cartridges"},
		{"search", "search the registry"},
		{"install", "install by name / file / url"},
		{"remove", "uninstall a cartridge"},
		{"registry", "manage registries"},
		{"trust", "manage publisher keys"},
		{"keygen", "make a signing keypair"},
		{"sign", "sign a cartridge file"},
		{"build", "author a cartridge"},
		{"where", "show the install dir"},
	},
	"cartridge registry": {{"add", "add a registry url/path"}, {"list", "list registries"}},
	"cartridge trust":    {{"add", "trust a publisher key"}, {"list", "list trusted keys"}},
	"learn":              {{"suggest", "show learning suggestions"}, {"promote", "promote a learned command"}, {"forget", "clear the learning log"}},
	"knowledge":          {{"install", "install a .sahayakpack"}, {"list", "list packs"}, {"search", "search packs"}, {"build", "build a pack"}, {"remove", "remove a pack"}},
	"memory":             {{"add", "add a note"}, {"list", "list notes"}, {"search", "search notes"}, {"forget", "forget a note"}},
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
	if strings.HasPrefix(before, "/") {
		// Palette: while still typing the command name (no space yet).
		if !strings.Contains(before, " ") {
			var out []Suggestion
			for _, c := range SlashCommands {
				if strings.HasPrefix(c.Name, before) {
					out = append(out, Suggestion{Value: c.Name + " ", Label: c.Name, Desc: c.Desc})
				}
			}
			return out, 0
		}
		// Argument completion: complete the subcommand for the current command path.
		fields := strings.Fields(before)
		endsSpace := strings.HasSuffix(before, " ")
		path := []string{strings.TrimPrefix(fields[0], "/")}
		partial := ""
		if endsSpace {
			path = append(path, fields[1:]...)
		} else {
			path = append(path, fields[1:len(fields)-1]...)
			partial = fields[len(fields)-1]
		}
		args, ok := slashArgs[strings.Join(path, " ")]
		if !ok {
			return nil, 0 // no known args at this level (e.g. a name/value arg)
		}
		lp := strings.ToLower(partial)
		var out []Suggestion
		for _, a := range args {
			if lp == "" || strings.HasPrefix(strings.ToLower(a.Name), lp) {
				out = append(out, Suggestion{Value: a.Name + " ", Label: a.Name, Desc: a.Desc})
			}
		}
		from := len(before)
		if !endsSpace {
			from = len(before) - len(partial)
		}
		return out, from
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
