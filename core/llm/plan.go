package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// placeholderRe matches unresolved placeholders like <pod>, <name>, <namespace>
// that a model emits when it doesn't yet know a real value. Such a command must
// never run — the value has to be discovered first.
var placeholderRe = regexp.MustCompile(`<[A-Za-z][\w .:/-]*>`)

// HasPlaceholder reports whether the step still contains an unresolved placeholder.
func (s Step) HasPlaceholder() bool {
	if placeholderRe.MatchString(s.Command) {
		return true
	}
	for _, a := range s.Args {
		if placeholderRe.MatchString(a) {
			return true
		}
	}
	return false
}

// Plan is the structured output the model must produce in response to a natural
// language request: a human-readable summary plus an ordered list of concrete,
// inspectable steps. The agent loop renders, gates, and executes these steps —
// the model never runs anything itself.
type Plan struct {
	// Summary is a one-line description of what the plan accomplishes.
	Summary string `json:"summary"`
	// Steps are executed in order, each gated according to its risk.
	Steps []Step `json:"steps"`
	// NeedMoreInfo, when non-empty, means the model needs clarification instead
	// of proposing commands; Steps should be empty in that case.
	NeedMoreInfo string `json:"need_more_info,omitempty"`
}

// Step is a single proposed command with the model's explanation of it.
type Step struct {
	// Command is the executable name, e.g. "systemctl".
	Command string `json:"command"`
	// Args are the arguments as a structured slice (NEVER a shell string) so the
	// runner can exec without an intermediate shell. e.g. ["reload","nginx"].
	Args []string `json:"args"`
	// Explanation tells the operator, in plain language, what this step does and
	// why — shown before the approval gate.
	Explanation string `json:"explanation"`
}

// Normalized repairs two common small-model mistakes so commands run correctly and
// classify correctly regardless of model quality:
//  1. The whole command line jammed into Command (e.g. "kubectl get pods -n x") —
//     it's split, with the extra fields prepended to Args.
//  2. A flag and its value jammed into ONE argument (e.g. "-n acme-dev" instead
//     of "-n","acme-dev"), which makes kubectl see a namespace with a leading
//     space and find nothing — split on the first space.
//
// Surrounding quotes/backticks on the command are also stripped.
func (s Step) Normalized() Step {
	cmd := strings.Trim(strings.TrimSpace(s.Command), "`'\"")
	if fields := strings.Fields(cmd); len(fields) > 1 {
		s.Command = fields[0]
		rest := fields[1:]
		// Guard against the model putting the full command line in BOTH Command and
		// Args, which would duplicate tokens. Args may exactly equal the command-line
		// args, or repeat only their tail (e.g. command "kubectl get cm -A -o name"
		// with args ["cm","-A","-o","name"]) — in either case Args is redundant.
		switch {
		case equalStrings(s.Args, rest), isSuffix(rest, s.Args):
			s.Args = rest
		case isSuffix(s.Args, rest):
			// Args already contains the command-line tokens at its tail; keep Args.
		default:
			s.Args = append(append([]string{}, rest...), s.Args...)
		}
	} else {
		s.Command = cmd
	}
	s.Args = collapseRepeatedTail(s.Args)
	s.Args = splitJammedFlags(s.Args)
	return s
}

// isSuffix reports whether slice a ends with the (non-empty) slice b.
func isSuffix(a, b []string) bool {
	if len(b) == 0 || len(b) > len(a) {
		return false
	}
	return equalStrings(a[len(a)-len(b):], b)
}

// collapseRepeatedTail removes a duplicated contiguous block at the end of args,
// e.g. ["get","cm","-A","-o","name","cm","-A","-o","name"] → ["get","cm","-A","-o",
// "name"]. This catches the model echoing a whole sub-command twice. It trims the
// largest such repeat (block length >= 2 to avoid eating a legitimate single
// repeated token like a duplicated value).
func collapseRepeatedTail(args []string) []string {
	for k := len(args) / 2; k >= 2; k-- {
		if equalStrings(args[len(args)-k:], args[len(args)-2*k:len(args)-k]) {
			return args[:len(args)-k]
		}
	}
	return args
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// splitJammedFlags splits arguments where a flag and its value were combined into
// one token, e.g. "-n acme-dev" → "-n","acme-dev". Only flag-shaped tokens
// (start with "-") that contain a space and have no "=" before that space are
// split; the value keeps any remaining spaces (e.g. a label selector).
func splitJammedFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			if i := strings.IndexByte(a, ' '); i > 0 && !strings.Contains(a[:i], "=") {
				out = append(out, a[:i], strings.TrimSpace(a[i+1:]))
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// Pretty renders the step's command line for display (best-effort quoting).
func (s Step) Pretty() string {
	parts := append([]string{s.Command}, s.Args...)
	for i, p := range parts {
		if strings.ContainsAny(p, " \t\"'") {
			parts[i] = fmt.Sprintf("%q", p)
		}
	}
	return strings.Join(parts, " ")
}

// Diagnosis is the structured output of the diagnose pass after a command fails.
type Diagnosis struct {
	// RootCause is the model's best explanation of why the command failed.
	RootCause string `json:"root_cause"`
	// Confidence is a coarse self-assessment: "high" | "medium" | "low".
	Confidence string `json:"confidence"`
	// NextStep, when non-nil, is a single follow-up command (re-enters the gate).
	NextStep *Step `json:"next_step,omitempty"`
}

// ParsePlan extracts a Plan from a model reply, tolerating models that wrap JSON
// in prose or markdown fences. Returns an error if no valid plan object is found.
func ParsePlan(raw string) (Plan, error) {
	var p Plan
	body, err := extractJSON(raw)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return p, fmt.Errorf("plan json invalid: %w", err)
	}
	for i := range p.Steps {
		p.Steps[i] = p.Steps[i].Normalized()
	}
	return p, nil
}

// ParseDiagnosis extracts a Diagnosis from a model reply with the same tolerance.
func ParseDiagnosis(raw string) (Diagnosis, error) {
	var d Diagnosis
	body, err := extractJSON(raw)
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		return d, fmt.Errorf("diagnosis json invalid: %w", err)
	}
	if d.NextStep != nil {
		n := d.NextStep.Normalized()
		d.NextStep = &n
	}
	return d, nil
}

// extractJSON returns the first balanced top-level JSON object found in s. Small
// models sometimes emit a ```json fence or a sentence before the object; this
// pulls out just the object so JSONOnly mode isn't strictly required to succeed.
func extractJSON(s string) (string, error) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", fmt.Errorf("no JSON object in model reply")
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// inside a string literal: ignore braces
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unbalanced JSON object in model reply")
}
