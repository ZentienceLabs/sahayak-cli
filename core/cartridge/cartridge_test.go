package cartridge

import (
	"strings"
	"testing"
)

func TestBuiltinsParse(t *testing.T) {
	carts, err := Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	names := map[string]bool{}
	for _, c := range carts {
		names[c.Name] = true
	}
	if !names["k8s"] || !names["systemd"] {
		t.Fatalf("expected k8s and systemd cartridges, got %v", names)
	}
}

func TestBuildSimpleTemplates(t *testing.T) {
	carts, _ := Builtins()
	k8s := carts[0]

	// list: command uses {resource}; selector is grounded for the processor, not the args.
	lt, ok := k8s.Find("list")
	if !ok {
		t.Fatal("no list template")
	}
	cmd, ok := lt.Build("list the configmaps for acme-web")
	if !ok {
		t.Fatal("list Build failed")
	}
	if cmd.Command != "kubectl" || strings.Join(cmd.Args, " ") != "get configmaps -A" {
		t.Errorf("list args = %v", cmd.Args)
	}
	if cmd.Values["selector"] != "acme-web" || cmd.Processor != "filter-summarize" {
		t.Errorf("list values/processor wrong: %+v / %s", cmd.Values, cmd.Processor)
	}

	// searchcfg: fixed command, keyword grounded for the processor.
	st, _ := k8s.Find("searchcfg")
	cmd, ok = st.Build("is there a config flag for telemetry")
	if !ok || strings.Join(cmd.Args, " ") != "get configmap -A -o json" {
		t.Fatalf("searchcfg build wrong: %v ok=%v", cmd.Args, ok)
	}
	if cmd.Values["keyword"] != "telemetry" {
		t.Errorf("searchcfg keyword = %q", cmd.Values["keyword"])
	}
}

func TestBuildDeclinesWhenSlotMissing(t *testing.T) {
	carts, _ := Builtins()
	k8s := carts[0]
	lt, _ := k8s.Find("list")
	// No resource noun → enum slot can't ground → decline (no half-blind command).
	if _, ok := lt.Build("show me everything for acme-web"); ok {
		t.Error("list should decline without a resource slot")
	}
	// No selector → decline.
	if _, ok := lt.Build("list configmaps"); ok {
		t.Error("list should decline without a selector slot")
	}
}

func TestParseRejectsCatalogWithoutTemplate(t *testing.T) {
	bad := `{"name":"x","catalog":[{"intent":"ghost","phrases":["do X"]}],"templates":[]}`
	if _, err := Parse([]byte(bad)); err == nil {
		t.Error("Parse should reject a catalog intent with no template")
	}
}

func TestChunkMarkdown(t *testing.T) {
	md := "# Redis\n\nRedis is a key-value store.\n\n## Restart\n\nTo restart redis, run systemctl restart redis.\n\nok"
	chunks := ChunkMarkdown(md)
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks (trivial 'ok' dropped, headings attached), got %d: %q", len(chunks), chunks)
	}
	if !strings.HasPrefix(chunks[0], "Redis\n") || !strings.Contains(chunks[1], "Restart") {
		t.Errorf("headings not attached to following block: %q", chunks)
	}
}

func TestComposeBuildsCartridge(t *testing.T) {
	tmpls := []Template{{Intent: "ping", Command: "redis-cli", Args: []string{"ping"}, Risk: "read-only", Shape: "simple"}}
	c := Compose("redis", "redis-cli ping", tmpls, nil, "# Redis\n\nA key-value store you can ping.")
	if c.Name != "redis" || len(c.Templates) != 1 {
		t.Fatalf("compose wrong: %+v", c)
	}
	if c.Applicability == nil || c.Applicability.Command != "redis-cli" {
		t.Errorf("applicability not parsed: %+v", c.Applicability)
	}
	if len(c.Knowledge) == 0 {
		t.Error("markdown KB not folded in")
	}
}

func TestParseRejectsTemplateWithoutCommand(t *testing.T) {
	bad := `{"name":"x","templates":[{"intent":"y"}]}`
	if _, err := Parse([]byte(bad)); err == nil {
		t.Error("Parse should reject a template with no command")
	}
}
