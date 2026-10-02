package agent

import (
	"fmt"

	"context"

	"github.com/ZentienceLabs/sahayak-cli/core/playbook"
)

// legacyRoute is the pre-cartridge deterministic pipeline: regex playbooks → composite
// matcher → semantic router → model classifier. It runs ONLY when the cartridge engine
// is disabled (SAHAYAK_LEGACY=1), kept for side-by-side comparison during the migration.
// The cartridge engine (tryCartridge) is the default and supersedes all of this.
func (a *Agent) legacyRoute(ctx context.Context, request string) (handled bool, err error) {
	if handled, err := a.tryPlaybook(ctx, request); err != nil || handled {
		return handled, err
	}
	if handled, err := a.tryComposite(ctx, request); err != nil || handled {
		return handled, err
	}
	if a.Router != nil {
		if m, ok, err := a.Router.Route(ctx, request); err != nil {
			a.UI.Note("semantic router unavailable (" + oneLine(err.Error()) + ") — continuing")
		} else if ok {
			a.UI.Note(fmt.Sprintf("routed to the %s playbook (matched %q ≈ %d%%)", m.Plan.Kind, m.Phrase, int(m.Score*100)))
			if handled, err := a.runPlan(ctx, m.Plan); err != nil || handled {
				return handled, err
			}
		}
	}
	if playbook.MightBeK8s(request) {
		if pl, ok := a.classifyIntent(ctx, request); ok {
			a.UI.Note("routed to the " + pl.Kind + " playbook")
			if handled, err := a.runPlan(ctx, pl); err != nil || handled {
				return handled, err
			}
		}
	}
	return false, nil
}
