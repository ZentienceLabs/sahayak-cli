// Package exec runs proposed commands safely and classifies their risk. The Go
// runtime — never the model — decides a command's risk tier, so a small/weak
// model cannot talk Sahayak into auto-running something destructive.
package exec

import "strings"

// Risk is the danger tier of a command. It drives whether the approval gate is
// mandatory and how loudly the command is presented.
type Risk int

const (
	// ReadOnly inspects state without changing it (ls, cat, kubectl get). May
	// auto-run when the operator opts in.
	ReadOnly Risk = iota
	// Mutating changes system state recoverably (systemctl reload, kubectl apply).
	// Always gated by default.
	Mutating
	// Destructive can cause data loss or outage (rm -rf, dd, drop, --force).
	// Always gated, loudly.
	Destructive
)

// String renders the tier for display.
func (r Risk) String() string {
	switch r {
	case ReadOnly:
		return "read-only"
	case Mutating:
		return "mutating"
	case Destructive:
		return "destructive"
	default:
		return "unknown"
	}
}

// readOnlyCommands are binaries that, with ordinary usage, only inspect state.
// Subcommands/flags can still escalate them (handled below), so this is a floor,
// not a guarantee.
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "less": true, "more": true, "head": true, "tail": true,
	"grep": true, "find": true, "stat": true, "file": true, "wc": true, "df": true,
	"du": true, "ps": true, "top": true, "free": true, "uptime": true, "whoami": true,
	"id": true, "hostname": true, "uname": true, "date": true, "env": true, "pwd": true,
	"which": true, "whereis": true, "echo": true, "dig": true, "nslookup": true,
	"ping": true, "ss": true, "netstat": true, "lsof": true, "journalctl": true,
	"systemctl-status": true, "ip": true, "uptimed": true,
}

// destructiveBinaries are dangerous on sight.
var destructiveBinaries = map[string]bool{
	"dd": true, "mkfs": true, "fdisk": true, "shred": true, "wipefs": true,
	"reboot": true, "shutdown": true, "halt": true, "poweroff": true,
}

// destructiveTokens, when present anywhere in command+args, force Destructive.
var destructiveTokens = []string{
	"rm -rf", "rm -fr", "--force", "--hard", "--no-preserve-root",
	"drop table", "drop database", "truncate", "delete from",
	"> /dev/sd", "format ", "del /f", "rmdir /s",
}

// mutatingSubcommands map a binary to subcommands that change state.
var mutatingSubcommands = map[string]map[string]bool{
	"kubectl": {"apply": true, "delete": true, "edit": true, "patch": true, "scale": true,
		"rollout": true, "cordon": true, "drain": true, "create": true, "replace": true, "label": true},
	"systemctl": {"start": true, "stop": true, "restart": true, "reload": true, "enable": true,
		"disable": true, "mask": true, "unmask": true, "kill": true},
	"docker": {"run": true, "rm": true, "rmi": true, "stop": true, "kill": true, "exec": true,
		"build": true, "push": true, "prune": true, "restart": true},
	"git":     {"push": true, "reset": true, "clean": true, "rebase": true, "checkout": true, "merge": true},
	"apt":     {"install": true, "remove": true, "purge": true, "upgrade": true, "autoremove": true},
	"apt-get": {"install": true, "remove": true, "purge": true, "upgrade": true, "autoremove": true},
	"yum":     {"install": true, "remove": true, "update": true, "erase": true},
	"dnf":     {"install": true, "remove": true, "update": true, "erase": true},
	"az":      {"create": true, "delete": true, "update": true, "set": true, "restart": true, "stop": true, "start": true},
}

// destructiveSubcommands map a binary to subcommands that risk loss/outage.
var destructiveSubcommands = map[string]map[string]bool{
	"kubectl": {"delete": true, "drain": true},
	"docker":  {"rm": true, "rmi": true, "prune": true},
	"git":     {"reset": true, "clean": true},
	"az":      {"delete": true},
}

// Classify returns the risk tier for a command + args. It is deliberately
// conservative: anything it cannot prove read-only is at least Mutating, and any
// destructive signal wins outright.
func Classify(command string, args []string) Risk {
	cmd := strings.ToLower(strings.TrimSpace(command))
	joined := strings.ToLower(cmd + " " + strings.Join(args, " "))

	// 1. Destructive tokens or binaries win immediately.
	if destructiveBinaries[cmd] {
		return Destructive
	}
	for _, tok := range destructiveTokens {
		if strings.Contains(joined, tok) {
			return Destructive
		}
	}
	// A bare `rm` with recursive/force flags is destructive even if tokens missed it.
	if cmd == "rm" && hasAnyFlag(args, "-r", "-rf", "-fr", "-f", "--recursive", "--force") {
		return Destructive
	}

	sub := firstSubcommand(args)
	if destructiveSubcommands[cmd][sub] {
		return Destructive
	}
	// `kubectl rollout status|history` only inspects a rollout — read-only — even
	// though `rollout restart|undo|pause|resume` mutate. Special-case before the
	// blanket "rollout is mutating" rule below.
	if cmd == "kubectl" && sub == "rollout" {
		if ss := secondSubcommand(args); ss == "status" || ss == "history" {
			return ReadOnly
		}
	}
	if mutatingSubcommands[cmd][sub] {
		return Mutating
	}

	// 2. Known read-only binaries — but only if no obvious write redirection.
	if readOnlyCommands[cmd] && !strings.ContainsAny(joined, ">") {
		// systemctl status / kubectl get etc. are read-only despite the binary
		// being multi-purpose; handle the common inspect subcommands.
		return ReadOnly
	}
	if cmd == "systemctl" && (sub == "status" || sub == "show" || sub == "list-units" || sub == "is-active") {
		return ReadOnly
	}
	if cmd == "kubectl" && (sub == "get" || sub == "describe" || sub == "logs" || sub == "top" || sub == "explain" || sub == "version") {
		return ReadOnly
	}
	if cmd == "docker" && (sub == "ps" || sub == "logs" || sub == "images" || sub == "inspect" || sub == "version") {
		return ReadOnly
	}
	if cmd == "git" && (sub == "status" || sub == "log" || sub == "diff" || sub == "show" || sub == "branch") {
		return ReadOnly
	}
	if cmd == "az" && sub == "list" {
		return ReadOnly
	}
	// nginx -t and similar validators are read-only.
	if cmd == "nginx" && hasAnyFlag(args, "-t", "-T") {
		return ReadOnly
	}

	// 3. Default: unknown command → treat as mutating (gate it).
	return Mutating
}

// firstSubcommand returns the first non-flag argument, lowercased.
func firstSubcommand(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return strings.ToLower(a)
		}
	}
	return ""
}

// secondSubcommand returns the second non-flag argument, lowercased (e.g. the
// "status" in `kubectl rollout status`).
func secondSubcommand(args []string) string {
	seen := 0
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if seen == 1 {
			return strings.ToLower(a)
		}
		seen++
	}
	return ""
}

func hasAnyFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}
