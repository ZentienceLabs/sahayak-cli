package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/cartridge"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/playbook"
)

// tryCartridge is the data-driven execution path: it routes a request through the
// cross-cartridge index (peer cartridges, matched by meaning), then runs the grounded
// template. "simple"-shape templates execute fully from cartridge data here (command +
// a named output processor); "resolve-fanout" templates delegate to the existing
// deployment-targeted runners for behavioral parity during the migration (those will be
// expressed as data in a later step). Returns handled=false to fall through.
//
// The reliability contract holds: the model authored nothing — the index picked an
// intent, the slot engine filled typed slots, and Go assembled the command from the
// human-authored template.
func (a *Agent) tryCartridge(ctx context.Context, request string) (handled bool, err error) {
	if a.Cartridges == nil || a.Cartridges.Empty() {
		return false, nil
	}
	a.ensureCartridgeApplicability(ctx)
	hit, ok, err := a.Cartridges.Route(ctx, request)
	if err != nil {
		a.UI.Note("cartridge index unavailable (" + oneLine(err.Error()) + ") — continuing")
		return false, nil
	}
	if !ok {
		return false, nil
	}
	a.UI.Note(fmt.Sprintf("cartridge %q → %s (matched %q ≈ %d%%)",
		hit.Cartridge.Name, hit.Intent, hit.Phrase, int(hit.Score*100)))

	switch hit.Template.Shape {
	case "", "simple":
		return a.runCartridgeSimple(ctx, hit)
	case "resolve-fanout":
		return a.runCartridgeFanout(ctx, hit)
	case "rollup":
		// Composition: reuse the status-rollup composer, fed by the grounded app slot.
		return a.runStatusComposite(ctx, playbook.Composite{
			Kind: "status", App: hit.Command.Values["app"], Parts: []string{"image", "rollout", "logs"},
		})
	default:
		return false, nil
	}
}

// ensureCartridgeApplicability runs each cartridge's applicability probe ONCE per
// session and tells the index which tools are present on this host — the deterministic
// peer-disambiguation prune (e.g. drop systemd when `systemctl` is absent, so "restart
// the X service" can't mis-route to a tool that isn't here). A cartridge with no probe is
// always applicable. Probes are read-only checks, run quietly (not gated).
func (a *Agent) ensureCartridgeApplicability(ctx context.Context) {
	if a.cartridgeProbed || a.Cartridges == nil {
		return
	}
	a.cartridgeProbed = true
	applicable := map[string]bool{}
	for _, c := range a.Cartridges.Cartridges() {
		if c.Applicability == nil {
			applicable[c.Name] = true
			continue
		}
		res := a.Runner.Run(ctx, c.Applicability.Command, c.Applicability.Args)
		applicable[c.Name] = res.Success()
		if !res.Success() {
			a.UI.Note(fmt.Sprintf("cartridge %q not applicable here (%s unavailable) — skipping it",
				c.Name, c.Applicability.Command))
		}
	}
	a.Cartridges.SetApplicable(applicable)
}

// runCartridgeSimple runs a one-command template and applies its named output processor.
// This path is fully data-driven — no per-intent Go beyond the shared processor library.
func (a *Agent) runCartridgeSimple(ctx context.Context, hit cartridge.Hit) (bool, error) {
	cmd := hit.Command
	step := llm.Step{
		Command:     cmd.Command,
		Args:        cmd.Args,
		Explanation: fmt.Sprintf("%s/%s", hit.Cartridge.Name, hit.Intent),
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
	if a.Learner != nil {
		a.Learner.Record("routed", "", cmd.Command, cmd.Args, hit.Cartridge.Name, hit.Intent, res.Success())
	}
	if !res.Success() {
		a.UI.Note("command failed — falling back to step-by-step investigation")
		return false, nil
	}
	out := a.Redactor.String(res.Stdout)
	if a.Env != nil {
		a.Env.LearnFromKubectl(cmd.Args, out)
	}

	switch cmd.Processor {
	case "filter-summarize":
		rows := playbook.FilterRows(out, cmd.Values["selector"])
		a.UI.Conclusion(playbook.Summarize(cmd.Values["resource"], cmd.Values["selector"], rows))
	case "configmap-search":
		a.UI.Conclusion(searchConfigmaps(out, cmd.Values["keyword"]))
	case "error-extract":
		a.UI.Conclusion(logErrorSummary(out))
	case "", "raw":
		a.UI.Conclusion(out)
	default:
		// Unknown processor: show the raw output rather than silently dropping it.
		a.UI.Conclusion(out)
	}
	return true, nil
}

// runCartridgeFanout runs a resolve-fanout template entirely from data: the top-level
// command resolves the matching deployments, then the template's `item` command runs once
// per deployment with {name}/{ns}/slots substituted, and each output passes its per-item
// processor before aggregation. No per-intent Go — image/rollout/restart/verifyenv/logs
// are all just different `item` commands + processors in the cartridge.
func (a *Agent) runCartridgeFanout(ctx context.Context, hit cartridge.Hit) (bool, error) {
	item := hit.Template.Item
	if item == nil {
		return false, nil
	}
	app := hit.Command.Values["app"]
	a.UI.Banner(fmt.Sprintf("%s/%s for %q", hit.Cartridge.Name, hit.Intent, app))

	deps, handled, err := a.resolveDeployments(ctx, app, 2)
	if deps == nil {
		return handled, err
	}
	extra := 0
	if len(deps) > maxLogTargets {
		extra = len(deps) - maxLogTargets
		deps = deps[:maxLogTargets]
	}

	var b strings.Builder
	for i, d := range deps {
		vals := map[string]string{"name": d.Name, "ns": d.Namespace}
		for k, v := range hit.Command.Values {
			vals[k] = v
		}
		args, ok := cartridge.Substitute(item.Args, vals)
		if !ok {
			continue
		}
		step := llm.Step{Command: item.Command, Args: args, Explanation: hit.Cartridge.Name + "/" + hit.Intent + " · " + d.Name}
		res, ran, lerr := a.runStepGated(ctx, step, i+1, len(deps)+1)
		if lerr != nil {
			if lerr == errAborted {
				a.UI.Note("stopped by operator.")
				return true, nil
			}
			return true, lerr
		}
		if !ran {
			continue
		}
		if a.Learner != nil {
			a.Learner.Record("routed", "", step.Command, step.Args, hit.Cartridge.Name, hit.Intent, res.Success())
		}
		out := a.Redactor.String(res.Stdout) + "\n" + a.Redactor.String(res.Stderr)
		fmt.Fprintf(&b, "▸ %s (namespace %s):\n%s\n\n", d.Name, d.Namespace, indentBlock(applyItemProcessor(item.Processor, out)))
	}
	if extra > 0 {
		fmt.Fprintf(&b, "(+%d more deployment(s) matched %q but were not scanned)\n", extra, app)
	}
	a.UI.Conclusion(strings.TrimRight(b.String(), "\n"))
	return true, nil
}

// applyItemProcessor runs a resolve-fanout item's per-item output processor.
func applyItemProcessor(name, out string) string {
	switch name {
	case "error-extract":
		return logErrorSummary(out)
	default: // "raw" / ""
		return strings.TrimSpace(out)
	}
}
