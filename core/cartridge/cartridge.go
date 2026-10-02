// Package cartridge defines the data model for Sahayak's tool cartridges and the
// generic runner that turns a command TEMPLATE + a user request into a grounded,
// ready-to-execute command — with no tool-specific code. A cartridge (k8s, az, docker,
// systemd…) is data: a catalog (phrasings → intent), command templates (intent →
// command + typed slots + risk), and an applicability probe. See CARTRIDGE-ARCHITECTURE.md.
//
// The reliability contract: the model never authors a command string. It picks an
// intent and the slot engine (core/slots) fills typed slots; this package assembles the
// command from the human-authored template. A template that can't ground a required
// slot declines, so a garbled request never becomes a half-blind command.
package cartridge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/slots"
)

// Template is one intent's command, authored as data.
type Template struct {
	Intent    string       `json:"intent"`              // e.g. "list", namespaced as "<cartridge>.<intent>" at load
	Command   string       `json:"command"`             // e.g. "kubectl"
	Args      []string     `json:"args"`                // may contain {slot} placeholders, e.g. "get", "{resource}", "-A"
	Slots     []slots.Spec `json:"slots,omitempty"`     // how to ground each placeholder
	Risk      string       `json:"risk,omitempty"`      // "", "read-only", "mutating", "destructive" (engine may re-classify)
	Processor string       `json:"processor,omitempty"` // optional named output processor (engine registry)
	Shape     string       `json:"shape,omitempty"`     // "simple" (default) | "resolve-fanout" | "rollup"
	Item      *ItemStep    `json:"item,omitempty"`      // for "resolve-fanout": the per-resolved-item command
}

// ItemStep is the per-item command of a resolve-fanout template: after the top-level
// command resolves a set (e.g. deployments matching an app), this runs once per item with
// {name}/{ns} (and the template's grounded slots) substituted in.
type ItemStep struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Risk      string   `json:"risk,omitempty"`
	Processor string   `json:"processor,omitempty"` // per-item: "raw" | "error-extract"
}

// Substitute replaces {name} placeholders in args from vals, returning ok=false if any
// placeholder is left unresolved (never emit a literal "{slot}"). Exported so the
// resolve-fanout runner can fill per-item args ({name}/{ns}) the same way Build does.
func Substitute(args []string, vals map[string]string) ([]string, bool) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		sub := placeholderRe.ReplaceAllStringFunc(a, func(m string) string {
			return vals[placeholderRe.FindStringSubmatch(m)[1]]
		})
		if placeholderRe.MatchString(sub) {
			return nil, false
		}
		out = append(out, sub)
	}
	return out, true
}

// CatalogEntry maps example phrasings to an intent (the router's per-cartridge data).
type CatalogEntry struct {
	Intent  string   `json:"intent"`
	Phrases []string `json:"phrases"`
}

// Probe is a cheap check answering "is this tool relevant on this machine?" — used for
// peer-cartridge disambiguation. The cartridge is applicable if the probe exits 0.
type Probe struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// Cartridge is one tool's data bundle: command templates (executable) PLUS curated KB
// (how-to / scenarios / facts as raw text chunks). The KB is stored as text — not
// pre-embedded — so a cartridge is portable across embedders (the host embeds it locally
// at load), which is what makes it shippable from a marketplace. A tool thus ships its
// commands and its knowledge together.
type Cartridge struct {
	Name          string         `json:"name"`
	Applicability *Probe         `json:"applicability,omitempty"`
	Catalog       []CatalogEntry `json:"catalog,omitempty"`
	Templates     []Template     `json:"templates,omitempty"`
	Knowledge     []string       `json:"knowledge,omitempty"` // curated how-to/scenario/fact chunks (raw text)
}

// Compose assembles a cartridge from its parts and chunks the markdown KB into Knowledge.
// This is the builder behind `cartridge build`: feed it a name, an optional applicability
// command, command templates, and a how-to markdown doc, and it returns a single
// self-describing cartridge (commands + knowledge) ready to install or publish.
func Compose(name, applicabilityCmd string, templates []Template, catalog []CatalogEntry, markdownKB string) Cartridge {
	c := Cartridge{Name: name, Templates: templates, Catalog: catalog, Knowledge: ChunkMarkdown(markdownKB)}
	if applicabilityCmd != "" {
		fields := strings.Fields(applicabilityCmd)
		c.Applicability = &Probe{Command: fields[0], Args: fields[1:]}
	}
	return c
}

// ChunkMarkdown splits a markdown/plain-text doc into KB chunks: blank-line-separated
// paragraphs, keeping a heading attached to the block that follows it, dropping trivial
// fragments. Deterministic and dependency-free — the same shape the knowledge packs use.
func ChunkMarkdown(md string) []string {
	var chunks []string
	var heading string
	for _, block := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n\n") {
		b := strings.TrimSpace(block)
		if b == "" {
			continue
		}
		// A lone heading line attaches to the next block for context.
		if strings.HasPrefix(b, "#") && !strings.Contains(b, "\n") {
			heading = strings.TrimLeft(b, "# ")
			continue
		}
		if heading != "" {
			b = heading + "\n" + b
			heading = ""
		}
		if len(b) >= 12 { // drop trivial fragments
			chunks = append(chunks, b)
		}
	}
	return chunks
}

// Command is a grounded, ready-to-run command produced from a Template. Values carries
// the grounded slots so an output processor can use ones the command line didn't (e.g.
// the "list" command is `get <res> -A` but the processor filters by the selector slot).
type Command struct {
	Command   string
	Args      []string
	Risk      string
	Processor string
	Values    map[string]string
}

var placeholderRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// Build grounds a Template against request: it extracts every slot and substitutes the
// values into the args. ok=false if a required slot can't be grounded OR a placeholder
// is left unresolved — never emit a command containing a literal "{slot}".
func (t Template) Build(request string) (Command, bool) {
	vals, ok := slots.ExtractAll(t.Slots, request)
	if !ok {
		return Command{}, false
	}
	args := make([]string, 0, len(t.Args))
	for _, a := range t.Args {
		sub := placeholderRe.ReplaceAllStringFunc(a, func(m string) string {
			return vals[placeholderRe.FindStringSubmatch(m)[1]]
		})
		if placeholderRe.MatchString(sub) || strings.Contains(sub, "{}") {
			return Command{}, false // an unresolved placeholder remained → decline
		}
		args = append(args, sub)
	}
	return Command{Command: t.Command, Args: args, Risk: t.Risk, Processor: t.Processor, Values: vals}, true
}

// Find returns the template for an intent (matching either the bare or namespaced form).
func (c Cartridge) Find(intent string) (Template, bool) {
	for _, t := range c.Templates {
		if t.Intent == intent || t.Intent == c.Name+"."+intent {
			return t, true
		}
	}
	return Template{}, false
}

// Parse loads one cartridge from JSON, validating that every catalog intent has a
// template and every template names a command — so a malformed pack fails loudly at
// load, not mid-request.
func Parse(data []byte) (Cartridge, error) {
	var c Cartridge
	if err := json.Unmarshal(data, &c); err != nil {
		return Cartridge{}, fmt.Errorf("cartridge: %w", err)
	}
	if strings.TrimSpace(c.Name) == "" {
		return Cartridge{}, fmt.Errorf("cartridge: missing name")
	}
	intents := map[string]bool{}
	for _, t := range c.Templates {
		if t.Intent == "" || t.Command == "" {
			return Cartridge{}, fmt.Errorf("cartridge %q: a template is missing intent or command", c.Name)
		}
		intents[t.Intent] = true
	}
	for _, e := range c.Catalog {
		if !intents[e.Intent] {
			return Cartridge{}, fmt.Errorf("cartridge %q: catalog intent %q has no template", c.Name, e.Intent)
		}
	}
	return c, nil
}
