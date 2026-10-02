package tui

import "testing"

func labels(s []Suggestion) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.Label
	}
	return out
}
func has(s []Suggestion, label string) bool {
	for _, x := range s {
		if x.Label == label {
			return true
		}
	}
	return false
}

func TestCompleteSlashPalette(t *testing.T) {
	all, from := Complete("/", Sources{})
	if from != 0 || len(all) != len(SlashCommands) {
		t.Fatalf("/ should list all slash commands from 0, got %d (from %d)", len(all), from)
	}
	mo, _ := Complete("/mo", Sources{})
	if !has(mo, "/model") || !has(mo, "/models") {
		t.Errorf("/mo should suggest /model and /models, got %v", labels(mo))
	}
	if v := mo[0].Value; v != "/model " && v != "/models " {
		t.Errorf("slash value should append a space for args, got %q", v)
	}
}

func TestCompleteSlashArgs(t *testing.T) {
	// A space after the command → subcommand completion.
	sub, from := Complete("/cartridge ", Sources{})
	if !has(sub, "install") || !has(sub, "list") || !has(sub, "registry") {
		t.Errorf("/cartridge <space> should list subcommands, got %v", labels(sub))
	}
	if from != len("/cartridge ") {
		t.Errorf("replaceFrom for empty partial should be end of input, got %d", from)
	}
	if sub[0].Value[len(sub[0].Value)-1] != ' ' {
		t.Errorf("subcommand value should end with a space, got %q", sub[0].Value)
	}
	// Filter by the partial.
	ins, from2 := Complete("/cartridge ins", Sources{})
	if len(ins) != 1 || ins[0].Label != "install" {
		t.Errorf("/cartridge ins → install only, got %v", labels(ins))
	}
	if from2 != len("/cartridge ") {
		t.Errorf("replaceFrom should be start of 'ins', got %d", from2)
	}
	// Nested: /cartridge registry → add/list.
	reg, _ := Complete("/cartridge registry ", Sources{})
	if !has(reg, "add") || !has(reg, "list") {
		t.Errorf("/cartridge registry → add/list, got %v", labels(reg))
	}
	// Other commands.
	if l, _ := Complete("/learn ", Sources{}); !has(l, "suggest") || !has(l, "promote") {
		t.Errorf("/learn → suggest/promote, got %v", labels(l))
	}
	// A name/value arg level has no completion.
	if s, _ := Complete("/cartridge install web", Sources{}); len(s) != 0 {
		t.Errorf("no completion for a name arg, got %v", labels(s))
	}
	// A command with no subcommands.
	if s, _ := Complete("/model ", Sources{}); len(s) != 0 {
		t.Errorf("/model has no subcommands, got %v", labels(s))
	}
}

func TestCompleteEntityPicker(t *testing.T) {
	src := Sources{
		Cartridges:  []string{"k8s", "systemd"},
		Deployments: []string{"web-api", "worker"},
		Namespaces:  []string{"staging", "demo"},
		EnvVars:     []string{"DEBUG_MODE"},
	}
	// bare "@" lists everything
	all, from := Complete("restart @", src)
	if from != len("restart ") {
		t.Errorf("replaceFrom should be the @ token start, got %d", from)
	}
	if len(all) != 7 {
		t.Errorf("@ should list all 7 entities, got %d: %v", len(all), labels(all))
	}
	// filtered
	w, _ := Complete("how is @web", src)
	if !has(w, "web-api") || has(w, "systemd") {
		t.Errorf("@web should match web-api only, got %v", labels(w))
	}
	if w[0].Value != "web-api" {
		t.Errorf("accepting an entity inserts the bare name, got %q", w[0].Value)
	}
	// a non-@ token yields nothing
	if s, _ := Complete("restart web", src); len(s) != 0 {
		t.Errorf("plain text should not autocomplete, got %v", labels(s))
	}
}

func TestCompleteEmpty(t *testing.T) {
	if s, _ := Complete("", Sources{}); len(s) != 0 {
		t.Errorf("empty input → no suggestions, got %v", labels(s))
	}
}
