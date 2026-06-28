package exec

import (
	"strings"
	"testing"
)

func TestDryRunArgs(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		args    []string
		wantOK  bool
		wantArg string // a substring that must appear in the result (when ok)
	}{
		{"rollout restart is dry-runnable", "kubectl", []string{"rollout", "restart", "deploy/x", "-n", "p"}, true, "--dry-run=server"},
		{"delete is dry-runnable", "kubectl", []string{"delete", "pod", "x", "-n", "p"}, true, "--dry-run=server"},
		{"apply is dry-runnable", "kubectl", []string{"apply", "-f", "x.yaml"}, true, "--dry-run=server"},
		{"patch is dry-runnable", "kubectl", []string{"patch", "deploy", "x", "-p", "{}"}, true, "--dry-run=server"},
		{"rollout status is NOT a mutation", "kubectl", []string{"rollout", "status", "deploy/x"}, false, ""},
		{"get is not dry-runnable here", "kubectl", []string{"get", "pods"}, false, ""},
		{"already has dry-run is skipped", "kubectl", []string{"apply", "-f", "x.yaml", "--dry-run=client"}, false, ""},
		{"non-kubectl is skipped", "helm", []string{"upgrade", "x"}, false, ""},
		{"exec is not dry-runnable", "kubectl", []string{"exec", "pod", "--", "printenv"}, false, ""},
	}
	for _, c := range cases {
		got, ok := DryRunArgs(c.cmd, c.args)
		if ok != c.wantOK {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.wantOK)
			continue
		}
		if c.wantOK {
			if !strings.Contains(strings.Join(got, " "), c.wantArg) {
				t.Errorf("%s: result %v missing %q", c.name, got, c.wantArg)
			}
			// The original args must be preserved (we only append).
			if len(got) != len(c.args)+1 {
				t.Errorf("%s: expected exactly one appended arg, got %v", c.name, got)
			}
		}
	}
}
