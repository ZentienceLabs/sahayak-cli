// Package diagnose turns a failed command into structured signals — exit-code
// meanings and recognized error patterns from stdout/stderr — BEFORE the model is
// consulted. Deterministic parsing grounds the model's root-cause analysis and, in
// clear cases, gives the operator an answer even with no model at all.
package diagnose

import (
	"fmt"
	"regexp"
	"strings"
)

// Signal is one recognized fact about a failure: what was detected, the evidence,
// and a plain-language hint at the likely cause / next move.
type Signal struct {
	// Kind is a short machine-ish category, e.g. "port-in-use", "oom", "exit-127".
	Kind string
	// Detail is the matched evidence (an excerpt), already trimmed.
	Detail string
	// Hint is a human explanation of the likely cause and direction to investigate.
	Hint string
}

// Report is the structured outcome of analyzing a failure.
type Report struct {
	ExitMeaning string   // human meaning of the exit code, if notable
	Signals     []Signal // recognized patterns, most-specific first
}

// HasFindings reports whether anything notable was detected.
func (r Report) HasFindings() bool { return r.ExitMeaning != "" || len(r.Signals) > 0 }

// PromptHints renders the report as a compact block to append to the diagnosis
// prompt, steering the model with what we already know deterministically.
func (r Report) PromptHints() string {
	if !r.HasFindings() {
		return ""
	}
	var b strings.Builder
	b.WriteString("Detected signals (deterministic, use to ground your analysis):\n")
	if r.ExitMeaning != "" {
		fmt.Fprintf(&b, "- exit code: %s\n", r.ExitMeaning)
	}
	for _, s := range r.Signals {
		fmt.Fprintf(&b, "- [%s] %s\n", s.Kind, s.Hint)
	}
	return b.String()
}

// Analyze inspects a finished command and returns recognized signals.
func Analyze(command string, exitCode int, stdout, stderr string) Report {
	r := Report{ExitMeaning: exitMeaning(exitCode)}
	haystack := stderr + "\n" + stdout
	for _, m := range matchers {
		if loc := m.re.FindStringIndex(haystack); loc != nil {
			r.Signals = append(r.Signals, Signal{
				Kind:   m.kind,
				Detail: excerpt(haystack, loc[0], loc[1]),
				Hint:   m.hint,
			})
		}
	}
	return r
}

// exitMeaning maps notable POSIX exit codes to human meaning. 0 and unknown codes
// return "".
func exitMeaning(code int) string {
	switch code {
	case 0:
		return ""
	case 1:
		return "1 — general error (the command ran but reported failure)"
	case 2:
		return "2 — misuse / syntax error in how the command was invoked"
	case 126:
		return "126 — command found but not executable (permissions, or not a binary)"
	case 127:
		return "127 — command not found (missing binary, or not on PATH)"
	case 130:
		return "130 — terminated by Ctrl-C (SIGINT)"
	case 137:
		return "137 — killed by SIGKILL (often the OOM killer: out of memory)"
	case 139:
		return "139 — segmentation fault (SIGSEGV)"
	case 143:
		return "143 — terminated by SIGTERM"
	case -1:
		return "process could not be started at all"
	default:
		if code > 128 {
			return fmt.Sprintf("%d — killed by signal %d", code, code-128)
		}
		return ""
	}
}

type matcher struct {
	kind string
	re   *regexp.Regexp
	hint string
}

// matchers are ordered most-specific first; the first match per kind wins.
var matchers = []matcher{
	{"exec-not-found", regexp.MustCompile(`(?i)(executable file not found|not found in %?PATH%?|command not found|: no such file or directory.*exec)`),
		"The program couldn't be launched — it's not on PATH (or the whole command line was mistakenly treated as the program name). Verify the binary name and that it's installed."},
	{"port-in-use", regexp.MustCompile(`(?i)(address already in use|bind\(\).*in use|EADDRINUSE|listen tcp.*address already in use)`),
		"A port is already held by another process. Find the holder (e.g. `ss -ltnp` / `lsof -i`) and stop it or choose another port."},
	{"permission-denied", regexp.MustCompile(`(?i)(permission denied|EACCES|operation not permitted|are you root|must be run as root)`),
		"Insufficient privileges. Check file ownership/mode or re-run with the right user/sudo."},
	{"no-such-file", regexp.MustCompile(`(?i)(no such file or directory|ENOENT|cannot stat|cannot open)`),
		"A referenced path does not exist. Verify the path, working directory, and that prerequisites created it."},
	{"conn-refused", regexp.MustCompile(`(?i)(connection refused|ECONNREFUSED|could not connect|failed to connect)`),
		"Target service isn't accepting connections. Confirm it's running, the port is right, and no firewall blocks it."},
	{"dns", regexp.MustCompile(`(?i)(name or service not known|could not resolve host|no such host|temporary failure in name resolution)`),
		"DNS resolution failed. Check the hostname, /etc/resolv.conf, and network reachability."},
	{"disk-full", regexp.MustCompile(`(?i)(no space left on device|ENOSPC|disk quota exceeded)`),
		"The filesystem is full. Free space or check `df -h` and large files (`du -sh *`)."},
	{"tls", regexp.MustCompile(`(?i)(x509|certificate verify failed|tls handshake|unknown authority|certificate has expired)`),
		"TLS/certificate problem. Check cert validity/expiry, the CA bundle, and clock skew."},
	// Kubernetes
	{"k8s-imagepull", regexp.MustCompile(`(?i)(ImagePullBackOff|ErrImagePull|manifest unknown|pull access denied)`),
		"Kubernetes can't pull the image. Verify the image name/tag, registry credentials (imagePullSecrets), and registry reachability."},
	{"k8s-crashloop", regexp.MustCompile(`(?i)CrashLoopBackOff`),
		"The container starts then crashes repeatedly. Inspect `kubectl logs --previous` and the container's command/health checks."},
	{"k8s-oomkilled", regexp.MustCompile(`(?i)OOMKilled`),
		"The container exceeded its memory limit and was killed. Raise the memory limit or fix the leak."},
	{"k8s-forbidden", regexp.MustCompile(`(?i)(Error from server \(Forbidden\)|is forbidden:|cannot .* in the namespace)`),
		"RBAC denied the action. Check the ServiceAccount/role bindings for the required verb on that resource."},
	{"k8s-notfound", regexp.MustCompile(`(?i)Error from server \(NotFound\)`),
		"The referenced Kubernetes object doesn't exist. Check the name/namespace/context."},
	// systemd
	{"systemd-failed", regexp.MustCompile(`(?i)(Job for .* failed|Failed to start|Unit .* not found|is masked)`),
		"A systemd unit failed or is missing/masked. Inspect `systemctl status <unit>` and `journalctl -u <unit>`."},
	// Package managers
	{"pkg-lock", regexp.MustCompile(`(?i)(Could not get lock|dpkg was interrupted|another process is using|/var/lib/dpkg/lock)`),
		"The package manager database is locked by another process. Wait for it, or recover with `dpkg --configure -a`."},
	{"pkg-notfound", regexp.MustCompile(`(?i)(Unable to locate package|No package .* available|404 +Not Found.*Packages)`),
		"Package not found. Refresh the index (`apt update` / `dnf makecache`) or check the package name/repo."},
	// Language stack traces
	{"trace-python", regexp.MustCompile(`(?m)^Traceback \(most recent call last\):`),
		"Python exception. The final `XError: message` line names the failure; the frames above show where."},
	{"trace-go", regexp.MustCompile(`(?i)(panic: |goroutine \d+ \[)`),
		"Go panic. The `panic:` line is the cause; the goroutine stack shows the call path."},
	{"trace-java", regexp.MustCompile(`(?m)^\s*Exception in thread|(?m)^\s*at [\w.$]+\(`),
		"Java exception. Read the top `Exception` line and the first `at` frame in your own package."},
	{"trace-node", regexp.MustCompile(`(?i)(UnhandledPromiseRejection|at Object\.<anonymous>|Error: .*\n\s+at )`),
		"Node.js error. The `Error:` line plus the first stack frame point to the failing call."},
}

// excerpt returns a trimmed window of text around a match for evidence display.
func excerpt(s string, start, end int) string {
	const pad = 40
	from := start - pad
	if from < 0 {
		from = 0
	}
	to := end + pad
	if to > len(s) {
		to = len(s)
	}
	return strings.TrimSpace(strings.ReplaceAll(s[from:to], "\n", " "))
}
