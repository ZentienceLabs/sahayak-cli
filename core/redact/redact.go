// Package redact scrubs secrets out of text before it is shown to the model or
// written to logs. It is a defense-in-depth layer for the sovereign promise: even
// though inference is local, command output and logs may contain tokens, keys, or
// passwords that should never be persisted or echoed back into a prompt.
package redact

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

const mask = "«REDACTED»"

// patterns are high-signal secret shapes. They are intentionally conservative to
// avoid mangling ordinary output; the env-value pass (below) catches the rest.
var patterns = []*regexp.Regexp{
	// Bearer / authorization tokens
	regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._\-]{12,}`),
	regexp.MustCompile(`(?i)\b(authorization)\s*[:=]\s*\S+`),
	// key=value / key: value for sensitive key names
	regexp.MustCompile(`(?i)\b(pass(word)?|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|client[_-]?secret)\b\s*[:=]\s*("[^"]*"|'[^']*'|\S+)`),
	// Cloud / provider key shapes
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),                                   // AWS access key id
	regexp.MustCompile(`\bASIA[0-9A-Z]{16}\b`),                                   // AWS temp key id
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),                         // GitHub tokens
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),                       // Slack tokens
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}\b`),                                // OpenAI-style keys
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`), // JWT
	// PEM private key blocks
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	// Connection strings with inline credentials: scheme://user:pass@host
	regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*://[^\s:@/]+):([^\s:@/]+)@`),
}

// sensitiveEnvNames mark env vars whose VALUES must be masked when they appear
// verbatim in text (e.g. a token printed by a command). Matching is by substring
// of the env var name, case-insensitive.
var sensitiveEnvNames = []string{
	"TOKEN", "SECRET", "PASSWORD", "PASSWD", "APIKEY", "API_KEY", "ACCESS_KEY",
	"PRIVATE_KEY", "CLIENT_SECRET", "AUTH", "CREDENTIAL", "SESSION", "COOKIE",
}

// Redactor masks secrets in text. Build one with New so the (cheap) env scan
// happens once per session.
type Redactor struct {
	envValues []string // sensitive env values, longest-first
}

// New builds a Redactor, snapshotting sensitive environment variable values so
// they can be masked wherever they surface in command output.
func New() *Redactor {
	var vals []string
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		name, val := kv[:eq], kv[eq+1:]
		if len(val) < 4 {
			continue // too short to be a meaningful secret; avoid false positives
		}
		upper := strings.ToUpper(name)
		for _, s := range sensitiveEnvNames {
			if strings.Contains(upper, s) {
				vals = append(vals, val)
				break
			}
		}
	}
	// Mask longer values first so substrings of a larger secret don't leak.
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	return &Redactor{envValues: vals}
}

// String returns s with all detected secrets replaced by a stable mask.
func (r *Redactor) String(s string) string {
	if s == "" {
		return s
	}
	out := s
	// 1. Exact sensitive env values (catches anything printed verbatim).
	for _, v := range r.envValues {
		out = strings.ReplaceAll(out, v, mask)
	}
	// 2. Structural patterns.
	for _, re := range patterns {
		out = re.ReplaceAllString(out, mask)
	}
	return out
}
