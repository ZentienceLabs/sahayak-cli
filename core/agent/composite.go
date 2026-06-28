package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/playbook"
)

// tryComposite handles higher-level, multi-playbook intents DETERMINISTICALLY: it
// recognizes a request that only makes sense as a synthesis of several atomic plays
// (e.g. "how is acme-web doing"), runs each part, and combines them into one
// verdict. The model never plans or authors a command — Go composes. Tried AFTER the
// atomic matchers (so a single-fact intent like "rollout status of X" wins) and before
// the router/classifier/loop.
func (a *Agent) tryComposite(ctx context.Context, request string) (handled bool, err error) {
	c, ok := playbook.MatchComposite(request)
	if !ok {
		return false, nil
	}
	switch c.Kind {
	case "status":
		return a.runStatusComposite(ctx, c)
	default:
		return false, nil
	}
}

// deployStatus is the per-deployment rollup gathered by runStatusComposite.
type deployStatus struct {
	name, namespace string
	image           string
	rolledOut       bool
	rolloutDetail   string
	logsClean       bool
	logIssues       string
	podsHealthy     bool
	podDetail       string
}

// deployPodHealth filters a `kubectl get pods` table to the pods belonging to a
// deployment (named "<deploy>-<rs-hash>-<pod-hash>") and returns whether they are all
// Running/Ready plus a compact detail line. A pod can be Running while its deployment
// reports "rolled out" — and vice versa — so this is an INDEPENDENT health signal, not a
// restatement of rollout. Restarts are noted but do NOT mark degraded on their own (they
// accumulate over a pod's lifetime and are often old/benign); only a not-Running/not-Ready
// pod is degraded. Zero matching pods is left to the rollout signal to judge.
func deployPodHealth(output, deployName string) (healthy bool, detail string) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return true, ""
	}
	col := map[string]int{}
	for i, h := range strings.Fields(lines[0]) {
		col[strings.ToUpper(h)] = i
	}
	si, ok := col["STATUS"]
	if !ok {
		return true, ""
	}
	ri, hasReady := col["READY"]
	rsi, hasRestarts := col["RESTARTS"]
	prefix := deployName + "-"

	var bad []string
	count, maxRestarts := 0, 0
	for _, l := range lines[1:] {
		f := strings.Fields(l)
		if len(f) <= si || !strings.HasPrefix(f[0], prefix) {
			continue
		}
		count++
		ready := ""
		if hasReady && ri < len(f) {
			ready = f[ri]
		}
		restarts := 0
		if hasRestarts && rsi < len(f) {
			restarts = leadingInt(f[rsi])
		}
		if restarts > maxRestarts {
			maxRestarts = restarts
		}
		if !healthyStatus(f[si]) || !readyOK(ready) {
			bad = append(bad, fmt.Sprintf("%s (%s %s)", f[0], ready, f[si]))
		}
	}
	switch {
	case count == 0:
		return true, "no pods scheduled"
	case len(bad) > 0:
		return false, fmt.Sprintf("%d/%d pods unhealthy: %s", len(bad), count, strings.Join(bad, ", "))
	case maxRestarts >= 5:
		return true, fmt.Sprintf("%d/%d pods Running (note: up to %d restarts)", count, count, maxRestarts)
	default:
		return true, fmt.Sprintf("%d/%d pods Running", count, count)
	}
}

// runStatusComposite answers "how is <app> doing" by composing three read-only plays
// per matched deployment — image (what's deployed), rollout (did it land), and a recent
// error scan (is it actually healthy) — then synthesizing a HEALTHY/DEGRADED verdict.
// This is the composition layer: each fact is already a deterministic playbook; the new
// value is running them together and reaching a combined conclusion in Go.
func (a *Agent) runStatusComposite(ctx context.Context, c playbook.Composite) (bool, error) {
	a.UI.Banner(`status rollup for "` + c.App + `" (image + rollout + recent errors)`)

	deps, handled, err := a.resolveDeployments(ctx, c.App, 2)
	if deps == nil {
		return handled, err
	}
	extra := 0
	if len(deps) > maxLogTargets {
		extra = len(deps) - maxLogTargets
		deps = deps[:maxLogTargets]
	}

	// 1 resolve step already ran; each deployment adds 4 read-only probes
	// (image, rollout, pod health, recent errors).
	total := 1 + len(deps)*4
	stepIdx := 1

	statuses := make([]deployStatus, 0, len(deps))
	for _, d := range deps {
		st := deployStatus{name: d.Name, namespace: d.Namespace}

		// Part 1 — image.
		imgStep := llm.Step{
			Command: "kubectl",
			Args: []string{"get", "deploy/" + d.Name, "-n", d.Namespace,
				"-o", `jsonpath={range .spec.template.spec.containers[*]}{.name}={.image}{"\n"}{end}`},
			Explanation: "read the container image(s) of " + d.Name + " in " + d.Namespace,
		}
		if res, ran, e := a.runStepGated(ctx, imgStep, stepIdx, total); e != nil {
			if e == errAborted {
				return true, nil
			}
			return true, e
		} else if ran {
			st.image = strings.TrimSpace(a.Redactor.String(res.Stdout))
		}
		stepIdx++

		// Part 2 — rollout status.
		roStep := llm.Step{
			Command:     "kubectl",
			Args:        []string{"rollout", "status", "deploy/" + d.Name, "-n", d.Namespace, "--timeout=10s"},
			Explanation: "check whether " + d.Name + " in " + d.Namespace + " finished rolling out",
		}
		if res, ran, e := a.runStepGated(ctx, roStep, stepIdx, total); e != nil {
			if e == errAborted {
				return true, nil
			}
			return true, e
		} else if ran {
			out := strings.TrimSpace(a.Redactor.String(res.Stdout) + " " + a.Redactor.String(res.Stderr))
			st.rolledOut = res.Success() && strings.Contains(strings.ToLower(out), "successfully rolled out")
			st.rolloutDetail = oneLine(out)
		}
		stepIdx++

		// Part 3 — pod health (independent of rollout: catches a Running deployment whose
		// pods crash-loop, or a "rolled out" one with not-Ready pods).
		podStep := llm.Step{
			Command:     "kubectl",
			Args:        []string{"get", "pods", "-n", d.Namespace},
			Explanation: "check pod health for " + d.Name + " in " + d.Namespace,
		}
		st.podsHealthy = true // default: don't flag if we couldn't read the table
		if res, ran, e := a.runStepGated(ctx, podStep, stepIdx, total); e != nil {
			if e == errAborted {
				return true, nil
			}
			return true, e
		} else if ran {
			st.podsHealthy, st.podDetail = deployPodHealth(a.Redactor.String(res.Stdout), d.Name)
		}
		stepIdx++

		// Part 4 — recent-error scan (shorter window than the logs play: a health check,
		// not a deep hunt).
		logStep := llm.Step{
			Command:     "kubectl",
			Args:        []string{"logs", "deploy/" + d.Name, "-n", d.Namespace, "--all-containers", "--since=1h", "--tail=200"},
			Explanation: "scan the last hour of " + d.Name + " logs for errors",
		}
		if res, ran, e := a.runStepGated(ctx, logStep, stepIdx, total); e != nil {
			if e == errAborted {
				return true, nil
			}
			return true, e
		} else if ran {
			out := a.Redactor.String(res.Stdout) + "\n" + a.Redactor.String(res.Stderr)
			summary := logErrorSummary(out)
			st.logsClean = strings.Contains(summary, "look clean")
			if !st.logsClean {
				st.logIssues = summary
			}
		}
		stepIdx++

		statuses = append(statuses, st)
	}

	a.UI.Conclusion(composeStatusConclusion(c.App, statuses, extra))
	return true, nil
}

// composeStatusConclusion synthesizes the gathered per-deployment facts into one answer
// with an explicit HEALTHY/DEGRADED verdict per namespace — the payoff of composition.
func composeStatusConclusion(app string, statuses []deployStatus, extra int) string {
	if len(statuses) == 0 {
		return fmt.Sprintf("Could not gather status for %q.", app)
	}
	// TL;DR first: how many namespaces are healthy, and which are not.
	healthy := 0
	var degradedNS []string
	for _, s := range statuses {
		if s.rolledOut && s.logsClean && s.podsHealthy {
			healthy++
		} else {
			degradedNS = append(degradedNS, s.namespace)
		}
	}
	var b strings.Builder
	if len(degradedNS) == 0 {
		fmt.Fprintf(&b, "%s: ✓ all %d deployment(s) HEALTHY.\n\n", app, len(statuses))
	} else {
		fmt.Fprintf(&b, "%s: %d/%d healthy — ⚠ DEGRADED in %s.\n\n", app, healthy, len(statuses), strings.Join(degradedNS, ", "))
	}

	for _, s := range statuses {
		degraded := !s.rolledOut || !s.logsClean || !s.podsHealthy
		verdict := "✓ HEALTHY"
		if degraded {
			verdict = "⚠ DEGRADED"
		}
		fmt.Fprintf(&b, "▸ %s (namespace %s): %s\n", s.name, s.namespace, verdict)
		if s.image != "" {
			fmt.Fprintf(&b, "    image:   %s\n", oneLine(s.image))
		}
		if s.rolledOut {
			fmt.Fprintf(&b, "    rollout: rolled out\n")
		} else {
			fmt.Fprintf(&b, "    rollout: NOT rolled out — %s\n", s.rolloutDetail)
		}
		if s.podDetail != "" {
			fmt.Fprintf(&b, "    pods:    %s\n", s.podDetail)
		}
		if s.logsClean {
			fmt.Fprintf(&b, "    logs:    no errors in the last hour\n")
		} else {
			fmt.Fprintf(&b, "    logs:    %s\n", indentTail(s.logIssues))
		}
	}
	if extra > 0 {
		fmt.Fprintf(&b, "(+%d more deployment(s) matched %q but were not scanned)\n", extra, app)
	}
	return strings.TrimRight(b.String(), "\n")
}

// indentTail keeps a multi-line log summary readable under the "logs:" label.
func indentTail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "             " + lines[i]
	}
	return strings.Join(lines, "\n")
}
