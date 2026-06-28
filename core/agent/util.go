package agent

import (
	"errors"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/exec"
)

// errAborted signals the operator rejected a step and the plan should stop. It is
// handled internally and not surfaced as a program error.
var errAborted = errors.New("plan aborted by operator")

// maxOutputLines caps how much command output is echoed/fed back, keeping the
// terminal and the diagnosis prompt readable.
const maxOutputLines = 40

// trimOutput trims trailing whitespace and clamps very long output to the last
// maxOutputLines lines (errors/stack traces are usually most informative at the end).
func trimOutput(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxOutputLines {
		return s
	}
	clipped := lines[len(lines)-maxOutputLines:]
	return "… (" + itoa(len(lines)-maxOutputLines) + " earlier lines omitted)\n" + strings.Join(clipped, "\n")
}

// effectiveStderr returns the command's stderr plus, when the process failed to
// start at all, the underlying error text (which carries the real reason, e.g.
// "executable file not found in PATH").
func effectiveStderr(res exec.Result) string {
	if res.Err != nil {
		if res.Stderr != "" {
			return res.Stderr + "\n" + res.Err.Error()
		}
		return res.Err.Error()
	}
	return res.Stderr
}

// oneLine collapses whitespace/newlines and clamps a snippet for compact prompt
// injection of retrieved documentation.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 240
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// indent prefixes each line with a small margin for readable nested output.
func indent(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "      " + l
	}
	return strings.Join(lines, "\n")
}

// itoa is a tiny strconv.Itoa to avoid an import in the hot trim path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
