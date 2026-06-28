package exec

import "strings"

// dryRunVerbs are kubectl mutating subcommands that support a SERVER-side dry run —
// the API server runs admission + validation and reports what *would* happen, without
// persisting anything. This lets Sahayak validate a mutation against the live cluster
// (does the object exist? is the patch well-formed? does admission allow it?) BEFORE
// running it for real — a deterministic critic whose verdict comes from the cluster,
// not from the model. `exec` (printenv) and `port-forward` are intentionally absent:
// they don't support dry-run.
var dryRunVerbs = map[string]bool{
	"apply": true, "create": true, "delete": true, "patch": true, "replace": true,
	"set": true, "scale": true, "annotate": true, "label": true, "expose": true,
	"autoscale": true, "rollout": true,
}

// rolloutDryRunnable are the `kubectl rollout <sub>` subcommands that accept dry-run.
var rolloutDryRunnable = map[string]bool{"restart": true, "undo": true, "scale": true}

// DryRunArgs reports whether a command is a kubectl mutation that supports a server
// dry run and, if so, returns a copy of args with `--dry-run=server` injected. It
// returns ok=false when the command isn't a dry-runnable kubectl mutation or already
// carries a --dry-run flag (so we never double it). Callers run the returned args
// first; only if that succeeds do they run the real command.
func DryRunArgs(command string, args []string) ([]string, bool) {
	if strings.ToLower(strings.TrimSpace(command)) != "kubectl" {
		return nil, false
	}
	for _, a := range args {
		if a == "--dry-run" || strings.HasPrefix(a, "--dry-run=") {
			return nil, false // already a dry run; don't re-wrap
		}
	}
	sub := firstSubcommand(args)
	if !dryRunVerbs[sub] {
		return nil, false
	}
	if sub == "rollout" && !rolloutDryRunnable[secondSubcommand(args)] {
		return nil, false // rollout status/history/pause/resume aren't mutations to validate
	}
	out := make([]string, len(args), len(args)+1)
	copy(out, args)
	return append(out, "--dry-run=server"), true
}
