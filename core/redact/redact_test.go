package redact

import (
	"os"
	"strings"
	"testing"
)

func TestRedactPatterns(t *testing.T) {
	r := New()
	cases := []struct {
		name string
		in   string
	}{
		{"bearer token", "Authorization: Bearer abcdef1234567890ABCDEF"},
		{"password kv", `password="hunter2supersecret"`},
		{"aws key", "AKIAIOSFODNN7EXAMPLE found in config"},
		{"github token", "ghp_0123456789abcdef0123456789abcdef0123"},
		{"openai key", "sk-abcdefghijklmnopqrstuvwxyz0123456789"},
		{"conn string", "postgres://admin:p4ssw0rd@db:5432/app"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := r.String(c.in)
			if !strings.Contains(out, mask) {
				t.Errorf("expected redaction in %q, got %q", c.in, out)
			}
		})
	}
}

func TestRedactEnvValue(t *testing.T) {
	os.Setenv("MY_API_TOKEN", "s3cr3t-value-xyz-987654")
	defer os.Unsetenv("MY_API_TOKEN")
	r := New()
	out := r.String("the deploy logged token s3cr3t-value-xyz-987654 oops")
	if strings.Contains(out, "s3cr3t-value-xyz-987654") {
		t.Errorf("env secret leaked: %q", out)
	}
	if !strings.Contains(out, mask) {
		t.Errorf("expected mask, got %q", out)
	}
}

func TestRedactLeavesOrdinaryTextAlone(t *testing.T) {
	r := New()
	in := "nginx: configuration file /etc/nginx/nginx.conf test is successful"
	if out := r.String(in); out != in {
		t.Errorf("ordinary text altered: %q -> %q", in, out)
	}
}
