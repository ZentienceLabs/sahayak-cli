package llm

import (
	"strings"
	"testing"
)

func TestParsePlan_Clean(t *testing.T) {
	raw := `{"summary":"reload nginx","steps":[{"command":"nginx","args":["-t"],"explanation":"validate"},{"command":"systemctl","args":["reload","nginx"],"explanation":"apply"}]}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Summary != "reload nginx" || len(p.Steps) != 2 {
		t.Fatalf("bad parse: %+v", p)
	}
	if p.Steps[0].Pretty() != "nginx -t" {
		t.Errorf("Pretty() = %q", p.Steps[0].Pretty())
	}
}

func TestParsePlan_FencedAndProse(t *testing.T) {
	// Small models often wrap JSON in prose or markdown fences; we must tolerate it.
	raw := "Sure! Here is the plan:\n```json\n{\"summary\":\"list\",\"steps\":[{\"command\":\"ls\",\"args\":[\"-la\"],\"explanation\":\"list files\"}]}\n```\nHope that helps."
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Steps) != 1 || p.Steps[0].Command != "ls" {
		t.Fatalf("bad parse: %+v", p)
	}
}

func TestParsePlan_BracesInsideStrings(t *testing.T) {
	// A brace inside a string value must not confuse the balanced-object scanner.
	raw := `{"summary":"print json","steps":[{"command":"echo","args":["{\"k\":1}"],"explanation":"emit {nested}"}]}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Steps) != 1 || p.Steps[0].Args[0] != `{"k":1}` {
		t.Fatalf("bad parse of nested braces: %+v", p)
	}
}

func TestParsePlanNormalizesFullCommandLine(t *testing.T) {
	// A weak model that puts the whole line in "command" must still produce a
	// runnable, correctly-classified step.
	raw := `{"summary":"list pods","steps":[{"command":"kubectl get pods -n acmedev","args":[],"explanation":"list"}]}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	s := p.Steps[0]
	if s.Command != "kubectl" {
		t.Fatalf("command not split: %q", s.Command)
	}
	want := []string{"get", "pods", "-n", "acmedev"}
	if len(s.Args) != 4 || s.Args[0] != "get" || s.Args[3] != "acmedev" {
		t.Fatalf("args = %v, want %v", s.Args, want)
	}
}

func TestNormalizedKeepsExistingArgs(t *testing.T) {
	s := Step{Command: "kubectl logs --previous", Args: []string{"mypod"}}.Normalized()
	if s.Command != "kubectl" || len(s.Args) != 3 || s.Args[2] != "mypod" {
		t.Fatalf("got %q %v", s.Command, s.Args)
	}
}

func TestNormalizedSplitsJammedFlag(t *testing.T) {
	// "-n acme-dev" as one arg must become two so kubectl gets a clean namespace.
	s := Step{Command: "kubectl", Args: []string{"get", "deployments", "-n acme-dev", "--output=wide"}}.Normalized()
	want := []string{"get", "deployments", "-n", "acme-dev", "--output=wide"}
	if strings.Join(s.Args, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", s.Args, want)
	}
	// --flag=value (no space) is left alone.
	s2 := Step{Command: "kubectl", Args: []string{"get", "pods", "--field-selector=status.phase=Running"}}.Normalized()
	if len(s2.Args) != 3 {
		t.Fatalf("should not split --flag=value: %v", s2.Args)
	}
}

func TestHasPlaceholder(t *testing.T) {
	if !(Step{Command: "kubectl", Args: []string{"logs", "--previous", "<pod>"}}).HasPlaceholder() {
		t.Error("should detect <pod>")
	}
	if !(Step{Command: "kubectl", Args: []string{"get", "pods", "-n", "<namespace>"}}).HasPlaceholder() {
		t.Error("should detect <namespace>")
	}
	if (Step{Command: "kubectl", Args: []string{"get", "pods", "-n", "acme-dev"}}).HasPlaceholder() {
		t.Error("false positive on a real command")
	}
}

func TestParseDiagnosis(t *testing.T) {
	raw := `{"root_cause":"port in use","confidence":"high","next_step":{"command":"ss","args":["-ltnp"],"explanation":"find holder"}}`
	d, err := ParseDiagnosis(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.RootCause != "port in use" || d.NextStep == nil || d.NextStep.Command != "ss" {
		t.Fatalf("bad diagnosis: %+v", d)
	}
}

// Reproduces the malformed step: model put the full command in Command AND echoed
// its tail in Args → "kubectl get configmap -A -o name configmap -A -o name".
func TestNormalizedDropsEchoedArgTail(t *testing.T) {
	s := Step{
		Command: "kubectl get configmap -A -o name",
		Args:    []string{"configmap", "-A", "-o", "name"},
	}.Normalized()
	want := []string{"get", "configmap", "-A", "-o", "name"}
	if s.Command != "kubectl" || !equalStrings(s.Args, want) {
		t.Fatalf("got %s %v, want kubectl %v", s.Command, s.Args, want)
	}
}

// Duplication already inside Args (Command was bare) must also collapse.
func TestNormalizedCollapsesRepeatedTailInArgs(t *testing.T) {
	s := Step{
		Command: "kubectl",
		Args:    []string{"get", "configmap", "-A", "-o", "name", "configmap", "-A", "-o", "name"},
	}.Normalized()
	want := []string{"get", "configmap", "-A", "-o", "name"}
	if !equalStrings(s.Args, want) {
		t.Fatalf("got %v, want %v", s.Args, want)
	}
}

// A legitimate command whose tail is not a duplicated block is left intact.
func TestNormalizedLeavesNonDuplicateArgs(t *testing.T) {
	s := Step{Command: "kubectl", Args: []string{"get", "pods", "-n", "acme-dev"}}.Normalized()
	if !equalStrings(s.Args, []string{"get", "pods", "-n", "acme-dev"}) {
		t.Fatalf("legit args were altered: %v", s.Args)
	}
}
