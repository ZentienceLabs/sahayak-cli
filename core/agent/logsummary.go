package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/exec"
)

// Logs are where small models drown: a single `kubectl logs` can be 150k lines of
// JSON. So Sahayak limits the fetch (--tail) and then extracts the error/warning
// lines deterministically into a short, human-readable LOG ANALYSIS — the model
// (and the operator) see the actual problems, not a wall of noise.

// isLogOutput reports whether a result is log output we should error-summarize.
func isLogOutput(res exec.Result) bool {
	cmd := strings.ToLower(res.Command)
	if cmd == "journalctl" {
		return true
	}
	if cmd == "kubectl" {
		for _, a := range res.Args {
			if strings.ToLower(a) == "logs" {
				return true
			}
		}
	}
	return false
}

// logErrorRe matches lines that indicate an error, warning, or stack trace.
var logErrorRe = regexp.MustCompile(`(?i)("level"\s*:\s*"(error|critical|fatal|warn(ing)?)"|\b(error|critical|fatal|exception|traceback|panic|stacktrace)\b|[A-Za-z]+Error\b|\bfailed\b|\bdenied\b|\btimed? ?out\b)`)

// idNoiseRe strips ids/timestamps/numbers so near-identical lines collapse to one
// signature (e.g. the same error logged 500 times with different request ids).
var idNoiseRe = regexp.MustCompile(`[0-9a-fA-F]{6,}|\d`)

// logErrorSummary extracts and de-duplicates the error/warning lines from log
// output into a compact assessment. Returns a "no errors" line if none are found.
func logErrorSummary(output string) string {
	lines := strings.Split(output, "\n")
	total := len(lines)

	var distinct []string
	seen := map[string]bool{}
	errCount := 0
	for _, l := range lines {
		if !logErrorRe.MatchString(l) {
			continue
		}
		errCount++
		sig := signature(l)
		if seen[sig] {
			continue
		}
		seen[sig] = true
		distinct = append(distinct, truncateLine(strings.TrimSpace(l), 220))
	}

	if errCount == 0 {
		return fmt.Sprintf("LOG ANALYSIS (by Sahayak): scanned %d log lines, found NO error/warning lines. This container's recent logs look clean.", total)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "LOG ANALYSIS (by Sahayak): scanned %d log lines; %d error/warning lines, %d distinct. Key issues:\n", total, errCount, len(distinct))
	const cap = 8
	for i, e := range distinct {
		if i >= cap {
			fmt.Fprintf(&b, "- …and %d more distinct error lines\n", len(distinct)-cap)
			break
		}
		fmt.Fprintf(&b, "- %s\n", e)
	}
	return strings.TrimSpace(b.String())
}

// signature normalizes a line (dropping ids/timestamps/numbers) for de-duplication.
func signature(l string) string {
	s := strings.ToLower(l)
	s = idNoiseRe.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func truncateLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
