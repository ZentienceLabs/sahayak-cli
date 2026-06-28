package diagnose

import (
	"strings"
	"testing"
)

func TestAnalyzeSignals(t *testing.T) {
	cases := []struct {
		name     string
		stderr   string
		wantKind string
	}{
		{"port in use", "nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)", "port-in-use"},
		{"permission", "open /etc/secret: permission denied", "permission-denied"},
		{"enoent", "cat: /no/such: No such file or directory", "no-such-file"},
		{"conn refused", "dial tcp 127.0.0.1:5432: connect: connection refused", "conn-refused"},
		{"disk full", "write /var/log/big: no space left on device", "disk-full"},
		{"k8s imagepull", "Failed to pull image: ErrImagePull", "k8s-imagepull"},
		{"k8s oom", "Last State: Terminated, Reason: OOMKilled", "k8s-oomkilled"},
		{"systemd", "Job for nginx.service failed because the control process exited", "systemd-failed"},
		{"python trace", "Traceback (most recent call last):\n  File x\nValueError: bad", "trace-python"},
		{"go panic", "panic: runtime error: index out of range\n\ngoroutine 1 [running]:", "trace-go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Analyze("cmd", 1, "", c.stderr)
			if !hasKind(r.Signals, c.wantKind) {
				t.Fatalf("expected signal %q, got %v", c.wantKind, kinds(r.Signals))
			}
		})
	}
}

func TestExitMeaning(t *testing.T) {
	if got := Analyze("x", 127, "", "").ExitMeaning; !strings.Contains(got, "not found") {
		t.Errorf("exit 127 meaning = %q", got)
	}
	if got := Analyze("x", 137, "", "").ExitMeaning; !strings.Contains(got, "OOM") && !strings.Contains(got, "SIGKILL") {
		t.Errorf("exit 137 meaning = %q", got)
	}
	if got := Analyze("x", 0, "", "").ExitMeaning; got != "" {
		t.Errorf("exit 0 should be empty, got %q", got)
	}
}

func TestPromptHints(t *testing.T) {
	r := Analyze("nginx", 1, "", "bind() to 0.0.0.0:80 failed (98: Address already in use)")
	hints := r.PromptHints()
	if !strings.Contains(hints, "port-in-use") || !strings.Contains(hints, "Detected signals") {
		t.Errorf("unexpected hints:\n%s", hints)
	}
}

func hasKind(sigs []Signal, kind string) bool {
	for _, s := range sigs {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func kinds(sigs []Signal) []string {
	var k []string
	for _, s := range sigs {
		k = append(k, s.Kind)
	}
	return k
}
