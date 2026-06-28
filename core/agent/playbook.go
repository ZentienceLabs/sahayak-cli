package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/playbook"
)

// tryPlaybook handles a small set of high-frequency, unambiguous intents
// DETERMINISTICALLY, bypassing the small model's plan/stop judgment entirely. These
// are the recurring moves from the operator's own debug runbook turned into Go:
//
//	"list <resource> for <app>"        -> one read-only listing, filtered + concluded
//	"why is <app> failing" / "<app> logs" -> resolve deployment, scan logs, extract errors
//
// Returns handled=true when the playbook owned the request (concluded, was stopped,
// or skipped). Returns handled=false to fall through to the adaptive investigate loop
// — either no playbook matched, or a deterministic step failed and the loop may still
// make progress.
func (a *Agent) tryPlaybook(ctx context.Context, request string) (handled bool, err error) {
	pl, ok := playbook.Match(request)
	if !ok {
		return false, nil
	}
	return a.runPlan(ctx, pl)
}

// runPlan dispatches a resolved Plan to its executor. Shared by the deterministic
// matcher (tryPlaybook) and the model-driven classifier fallback (classifyIntent).
func (a *Agent) runPlan(ctx context.Context, pl playbook.Plan) (handled bool, err error) {
	switch pl.Kind {
	case "list":
		return a.runListPlaybook(ctx, pl)
	case "logs":
		return a.runLogsPlaybook(ctx, pl)
	case "image":
		return a.runImagePlaybook(ctx, pl)
	case "rollout":
		return a.runRolloutPlaybook(ctx, pl)
	case "restart":
		return a.runRestartPlaybook(ctx, pl)
	case "verifyenv":
		return a.runVerifyEnvPlaybook(ctx, pl)
	case "searchcfg":
		return a.runSearchConfigPlaybook(ctx, pl)
	case "status":
		// Composition kind: expand the routed Plan into the multi-play status rollup.
		return a.runStatusComposite(ctx, playbook.Composite{
			Kind: "status", App: pl.App, Parts: []string{"image", "rollout", "logs"},
		})
	default:
		return false, nil
	}
}

// resolveDeployments runs the shared first step of every deployment-targeted
// playbook: list deployments across all namespaces and return the ones matching the
// app. ok=false means the caller already concluded or should fall back (the bool it
// returns mirrors tryPlaybook's handled/error contract via the returned values).
//
// Returns (deps, handled, err): when deps is nil, handled/err tell tryPlaybook what
// to do (handled=true means we concluded or were stopped; handled=false means fall
// through to the adaptive loop).
func (a *Agent) resolveDeployments(ctx context.Context, app string, steps int) (deps []playbook.Row, handled bool, err error) {
	getDeploy := llm.Step{
		Command:     "kubectl",
		Args:        []string{"get", "deployments", "-A"},
		Explanation: `find the deployment(s) matching "` + app + `" and their namespace`,
	}
	res, ran, err := a.runStepGated(ctx, getDeploy, 0, steps)
	if err != nil {
		if err == errAborted {
			a.UI.Note("stopped by operator.")
			return nil, true, nil
		}
		return nil, true, err
	}
	if !ran {
		return nil, true, nil
	}
	if !res.Success() {
		a.UI.Note("could not list deployments — falling back to step-by-step investigation")
		return nil, false, nil
	}
	table := a.Redactor.String(res.Stdout)
	if a.Env != nil {
		a.Env.LearnFromKubectl(getDeploy.Args, table)
	}
	deps = playbook.FilterRows(table, app)
	if len(deps) == 0 {
		a.UI.Conclusion(fmt.Sprintf("No deployment matching %q exists in any namespace (checked all namespaces).", app))
		return nil, true, nil
	}
	return deps, false, nil
}

// runListPlaybook answers "list <resource> for <app>" with ONE read-only
// `kubectl get <res> -A`, filtered and concluded in Go.
func (a *Agent) runListPlaybook(ctx context.Context, pl playbook.Plan) (bool, error) {
	a.UI.Banner("recognized: list " + pl.Display + ` matching "` + pl.Selector + `"`)

	step := llm.Step{
		Command:     "kubectl",
		Args:        []string{"get", pl.Resource, "-A"},
		Explanation: "list all " + pl.Display + ` across every namespace, then match "` + pl.Selector + `" locally`,
	}
	res, ran, err := a.runStepGated(ctx, step, 0, 1)
	if err != nil {
		if err == errAborted {
			a.UI.Note("stopped by operator.")
			return true, nil
		}
		return true, err
	}
	if !ran {
		return true, nil
	}
	if !res.Success() {
		a.UI.Note("listing failed — falling back to step-by-step investigation")
		return false, nil
	}

	table := a.Redactor.String(res.Stdout)
	if a.Env != nil {
		if learned := a.Env.LearnFromKubectl(step.Args, table); learned > 0 {
			a.UI.Note(fmt.Sprintf("remembered %d stable fact(s) about your environment", learned))
		}
	}
	rows := playbook.FilterRows(table, pl.Selector)
	a.UI.Conclusion(playbook.Summarize(pl.Display, pl.Selector, rows))
	return true, nil
}

// maxLogTargets caps how many matched deployments we pull logs from, so a broad app
// keyword can't fan out to dozens of log fetches.
const maxLogTargets = 3

// runLogsPlaybook answers "why is <app> failing" / "<app> logs" the way the runbook
// does it: resolve the deployment(s) matching <app> across all namespaces, then read
// each one's recent logs (time-bounded, capped) and extract the DISTINCT error lines
// in Go — optionally narrowed to a subsystem the operator named (the Focus keywords).
// Every step is read-only, so it auto-runs; the model is never consulted.
func (a *Agent) runLogsPlaybook(ctx context.Context, pl playbook.Plan) (bool, error) {
	banner := `diagnosing logs for "` + pl.App + `"`
	if len(pl.Focus) > 0 {
		banner += " (focus: " + strings.Join(pl.Focus, ", ") + ")"
	}
	a.UI.Banner(banner)

	// Step 1 — resolve the workload (find deployment + its namespace).
	deps, handled, err := a.resolveDeployments(ctx, pl.App, 2)
	if deps == nil {
		return handled, err
	}
	extra := 0
	if len(deps) > maxLogTargets {
		extra = len(deps) - maxLogTargets
		deps = deps[:maxLogTargets]
	}

	// Step 2..N — read and digest each deployment's recent logs.
	var b strings.Builder
	for i, d := range deps {
		logStep := llm.Step{
			Command:     "kubectl",
			Args:        []string{"logs", "deploy/" + d.Name, "-n", d.Namespace, "--all-containers", "--since=3h", "--tail=500"},
			Explanation: "read the last 3h of logs for " + d.Name + " in " + d.Namespace,
		}
		lres, lran, lerr := a.runStepGated(ctx, logStep, i+1, len(deps)+1)
		if lerr != nil {
			if lerr == errAborted {
				a.UI.Note("stopped by operator.")
				return true, nil
			}
			return true, lerr
		}
		if !lran {
			continue
		}
		out := a.Redactor.String(lres.Stdout) + "\n" + a.Redactor.String(lres.Stderr)
		fmt.Fprintf(&b, "▸ %s (namespace %s):\n%s\n\n", d.Name, d.Namespace, digestLogs(out, pl.Focus))
	}
	if extra > 0 {
		fmt.Fprintf(&b, "(+%d more deployment(s) matched %q but were not scanned)\n", extra, pl.App)
	}
	a.UI.Conclusion(strings.TrimRight(b.String(), "\n"))
	return true, nil
}

// runImagePlaybook answers "what image is <app> running": resolve the deployment(s),
// then read each one's container images via a read-only jsonpath get.
func (a *Agent) runImagePlaybook(ctx context.Context, pl playbook.Plan) (bool, error) {
	a.UI.Banner(`image of "` + pl.App + `"`)
	deps, handled, err := a.resolveDeployments(ctx, pl.App, 2)
	if deps == nil {
		return handled, err
	}
	var b strings.Builder
	for i, d := range deps {
		step := llm.Step{
			Command: "kubectl",
			Args: []string{"get", "deploy/" + d.Name, "-n", d.Namespace,
				"-o", `jsonpath={range .spec.template.spec.containers[*]}{.name}={.image}{"\n"}{end}`},
			Explanation: "read the container image(s) of " + d.Name + " in " + d.Namespace,
		}
		res, ran, lerr := a.runStepGated(ctx, step, i+1, len(deps)+1)
		if lerr != nil {
			if lerr == errAborted {
				return true, nil
			}
			return true, lerr
		}
		if !ran {
			continue
		}
		imgs := strings.TrimSpace(a.Redactor.String(res.Stdout))
		if imgs == "" {
			imgs = "(no image reported)"
		}
		fmt.Fprintf(&b, "▸ %s (namespace %s):\n%s\n\n", d.Name, d.Namespace, indentBlock(imgs))
	}
	a.UI.Conclusion(strings.TrimRight(b.String(), "\n"))
	return true, nil
}

// runRolloutPlaybook answers "rollout status of <app>": resolve the deployment(s),
// then read each one's rollout status (read-only, bounded by --timeout).
func (a *Agent) runRolloutPlaybook(ctx context.Context, pl playbook.Plan) (bool, error) {
	a.UI.Banner(`rollout status of "` + pl.App + `"`)
	deps, handled, err := a.resolveDeployments(ctx, pl.App, 2)
	if deps == nil {
		return handled, err
	}
	var b strings.Builder
	for i, d := range deps {
		step := llm.Step{
			Command:     "kubectl",
			Args:        []string{"rollout", "status", "deploy/" + d.Name, "-n", d.Namespace, "--timeout=10s"},
			Explanation: "check the rollout status of " + d.Name + " in " + d.Namespace,
		}
		res, ran, lerr := a.runStepGated(ctx, step, i+1, len(deps)+1)
		if lerr != nil {
			if lerr == errAborted {
				return true, nil
			}
			return true, lerr
		}
		if !ran {
			continue
		}
		// `rollout status` exits non-zero on timeout (still rolling / stuck) — report
		// that honestly rather than as a hard error.
		out := strings.TrimSpace(a.Redactor.String(res.Stdout) + " " + a.Redactor.String(res.Stderr))
		if out == "" {
			out = "(no status reported)"
		}
		fmt.Fprintf(&b, "▸ %s (namespace %s): %s\n", d.Name, d.Namespace, out)
	}
	a.UI.Conclusion(strings.TrimRight(b.String(), "\n"))
	return true, nil
}

// runRestartPlaybook answers "restart <app>" (§B1 of the runbook). This MUTATES, so
// each `rollout restart` is classified Mutating and goes through the approval gate —
// it never auto-runs. After a successful restart it reports the new rollout status.
func (a *Agent) runRestartPlaybook(ctx context.Context, pl playbook.Plan) (bool, error) {
	a.UI.Banner(`restart "` + pl.App + `"`)
	deps, handled, err := a.resolveDeployments(ctx, pl.App, 2)
	if deps == nil {
		return handled, err
	}
	var b strings.Builder
	for i, d := range deps {
		restart := llm.Step{
			Command:     "kubectl",
			Args:        []string{"rollout", "restart", "deploy/" + d.Name, "-n", d.Namespace},
			Explanation: "roll the pods of " + d.Name + " in " + d.Namespace + " (picks up changed ConfigMaps/Secrets)",
		}
		res, ran, rerr := a.runStepGated(ctx, restart, i+1, len(deps)+1)
		if rerr != nil {
			if rerr == errAborted {
				a.UI.Note("stopped by operator.")
				return true, nil
			}
			return true, rerr
		}
		if !ran {
			fmt.Fprintf(&b, "▸ %s (namespace %s): skipped\n", d.Name, d.Namespace)
			continue
		}
		status := "restarted"
		if !res.Success() {
			status = "restart FAILED — " + oneLine(a.Redactor.String(effectiveStderr(res)))
		}
		fmt.Fprintf(&b, "▸ %s (namespace %s): %s\n", d.Name, d.Namespace, status)
	}
	a.UI.Conclusion(strings.TrimRight(b.String(), "\n"))
	return true, nil
}

// runVerifyEnvPlaybook answers "is <ENV_VAR> set in <app>" (§B2): resolve the
// deployment(s), then exec `printenv <VAR>` in a running pod to prove the value
// reached the container, not just the manifest. exec is classified Mutating, so it
// goes through the approval gate.
func (a *Agent) runVerifyEnvPlaybook(ctx context.Context, pl playbook.Plan) (bool, error) {
	a.UI.Banner(`verify env ` + pl.EnvVar + ` in "` + pl.App + `"`)
	deps, handled, err := a.resolveDeployments(ctx, pl.App, 2)
	if deps == nil {
		return handled, err
	}
	var b strings.Builder
	for i, d := range deps {
		step := llm.Step{
			Command:     "kubectl",
			Args:        []string{"exec", "deploy/" + d.Name, "-n", d.Namespace, "--", "printenv", pl.EnvVar},
			Explanation: "read " + pl.EnvVar + " from a running " + d.Name + " pod in " + d.Namespace,
		}
		res, ran, rerr := a.runStepGated(ctx, step, i+1, len(deps)+1)
		if rerr != nil {
			if rerr == errAborted {
				a.UI.Note("stopped by operator.")
				return true, nil
			}
			return true, rerr
		}
		if !ran {
			fmt.Fprintf(&b, "▸ %s (namespace %s): skipped\n", d.Name, d.Namespace)
			continue
		}
		val := strings.TrimSpace(a.Redactor.String(res.Stdout))
		switch {
		case !res.Success():
			// printenv exits 1 when the variable is unset.
			fmt.Fprintf(&b, "▸ %s (namespace %s): %s is NOT set\n", d.Name, d.Namespace, pl.EnvVar)
		case val == "":
			fmt.Fprintf(&b, "▸ %s (namespace %s): %s is set but empty\n", d.Name, d.Namespace, pl.EnvVar)
		default:
			fmt.Fprintf(&b, "▸ %s (namespace %s): %s=%s\n", d.Name, d.Namespace, pl.EnvVar, val)
		}
	}
	a.UI.Conclusion(strings.TrimRight(b.String(), "\n"))
	return true, nil
}

// runSearchConfigPlaybook answers "is there a config key / env var related to
// <keyword>": it dumps all configmaps as JSON (read-only) and searches their names,
// data KEYS and VALUES for the keyword IN GO — the grep-over-config-content task the
// model mangles with jsonpath. Reports the matching configmap, namespace, and which
// keys matched (with the value when it's short and not a secret-looking blob).
func (a *Agent) runSearchConfigPlaybook(ctx context.Context, pl playbook.Plan) (bool, error) {
	a.UI.Banner(`searching configmaps for "` + pl.Keyword + `"`)
	step := llm.Step{
		Command:     "kubectl",
		Args:        []string{"get", "configmap", "-A", "-o", "json"},
		Explanation: `dump all configmaps, then search their keys/values for "` + pl.Keyword + `" locally`,
	}
	res, ran, err := a.runStepGated(ctx, step, 0, 1)
	if err != nil {
		if err == errAborted {
			a.UI.Note("stopped by operator.")
			return true, nil
		}
		return true, err
	}
	if !ran {
		return true, nil
	}
	if !res.Success() {
		a.UI.Note("could not read configmaps — falling back to step-by-step investigation")
		return false, nil
	}
	a.UI.Conclusion(searchConfigmaps(a.Redactor.String(res.Stdout), pl.Keyword))
	return true, nil
}

// searchConfigmaps parses `kubectl get configmap -A -o json` and returns a report of
// every configmap whose name, a data key, or a data value contains the keyword.
func searchConfigmaps(jsonOut, keyword string) string {
	kw := strings.ToLower(keyword)
	var doc struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Data map[string]string `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		return fmt.Sprintf("could not parse configmap data to search for %q.", keyword)
	}

	type hit struct {
		ns, name string
		keys     []string
	}
	var hits []hit
	for _, it := range doc.Items {
		var matched []string
		nameMatch := strings.Contains(strings.ToLower(it.Metadata.Name), kw)
		for k, v := range it.Data {
			if strings.Contains(strings.ToLower(k), kw) || strings.Contains(strings.ToLower(v), kw) {
				entry := k
				if len(v) <= 60 && !strings.ContainsAny(v, "\n") {
					entry = k + "=" + v
				}
				matched = append(matched, entry)
			}
		}
		if nameMatch || len(matched) > 0 {
			sort.Strings(matched)
			hits = append(hits, hit{ns: it.Metadata.Namespace, name: it.Metadata.Name, keys: matched})
		}
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No configmap key or value matching %q found in any namespace (searched every configmap's contents).", keyword)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].name != hits[j].name {
			return hits[i].name < hits[j].name
		}
		return hits[i].ns < hits[j].ns
	})
	var b strings.Builder
	fmt.Fprintf(&b, "Found %q in %d configmap(s):\n", keyword, len(hits))
	for _, h := range hits {
		if len(h.keys) == 0 {
			fmt.Fprintf(&b, "  - %s  (namespace %s) — matches the configmap name\n", h.name, h.ns)
			continue
		}
		fmt.Fprintf(&b, "  - %s  (namespace %s) — keys: %s\n", h.name, h.ns, strings.Join(h.keys, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// digestLogs extracts the distinct error/warning lines from raw log output. When the
// operator named a subsystem (Focus), it prefers the focus-narrowed digest — but only
// if that subset actually contains issues, so real errors outside the named subsystem
// are never hidden.
func digestLogs(out string, focus []string) string {
	full := logErrorSummary(out)
	if len(focus) == 0 {
		return full
	}
	if narrowed := logErrorSummary(focusSubset(out, focus)); strings.Contains(narrowed, "Key issues") {
		return narrowed
	}
	return full
}

// focusSubset keeps only the log lines mentioning a focus keyword. If nothing matches
// it returns the full text unchanged (don't filter away everything).
func focusSubset(out string, focus []string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		ll := strings.ToLower(l)
		for _, f := range focus {
			if strings.Contains(ll, f) {
				keep = append(keep, l)
				break
			}
		}
	}
	if len(keep) == 0 {
		return out
	}
	return strings.Join(keep, "\n")
}
