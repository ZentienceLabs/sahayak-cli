package playbook

import "testing"

// This is the golden ROUTING regression suite: a single corpus of real operator
// phrasings (seeded from the acme debug runbook and from prior live failures)
// mapped to the intent each MUST route to — or "" meaning "no playbook, fall through
// to the adaptive loop". Every change to the matchers runs against this, so routing
// behavior is locked and a regression in one intent can't silently break another.
//
// When you teach Sahayak a new phrasing, add a line here FIRST (it should fail),
// then make it pass. That keeps coverage honest and visible.
var routingCorpus = []struct {
	req  string
	kind string // expected Plan.Kind, or "" for no-match
	app  string // expected App or Selector (whichever the intent uses); "" = don't check
}{
	// --- list (incl. the exact historical "empty list" failure) ---
	{"can you provide configmap list for acme-web", "list", "acme-web"},
	{"list configmaps for acme-web", "list", "acme-web"},
	{"show me the services in acme-dev", "list", "acme-dev"},
	{"get deployments for acme", "list", "acme"},
	{"list all secrets in kube-system", "list", "kube-system"},
	{"which pods of acme-worker are there", "list", "acme-worker"},

	// --- logs / diagnostics (runbook §A) ---
	{"why is acme-web failing", "logs", "acme-web"},
	{"show me errors in acme-web logs", "logs", "acme-web"},
	{"acme-worker is crashing", "logs", "acme-worker"},
	{"why is github integration failing in acme-web", "logs", "acme-web"},
	{"debug the acme-web logs for github oauth", "logs", "acme-web"},

	// --- image ---
	{"what image is acme-web running", "image", "acme-web"},
	{"show the image tag for acme-ui", "image", "acme-ui"},

	// --- rollout status ---
	{"rollout status of acme-web", "rollout", "acme-web"},
	{"is acme-worker rolled out", "rollout", "acme-worker"},

	// --- restart (runbook §B1, mutating) ---
	{"restart acme-web", "restart", "acme-web"},
	{"please redeploy acme-worker", "restart", "acme-worker"},
	{"restart acme-web because it is failing", "restart", "acme-web"},

	// --- verify env (runbook §B2) ---
	{"is CONSOLE_WORKFLOW_REDESIGN set in acme-web", "verifyenv", "acme-web"},
	{"verify FEATURE_X_ENABLED in acme-web", "verifyenv", "acme-web"},

	// --- must NOT fire (hand off to the adaptive loop / approval) ---
	{"why did my pod crash in acme-dev", "", ""},                  // pod-level
	{"are there any errors in acme dev", "", ""},                  // no hyphenated app
	{"list failing pods across the cluster", "", ""},                 // no entity
	{"show me all namespaces", "", ""},                               // cluster-scoped
	{"configmap acme-web", "", ""},                                // no verb
	{"list configmaps in the cluster", "", ""},                       // noise selector
	{"set CONSOLE_WORKFLOW_REDESIGN to true in acme-web", "", ""}, // assignment, not read
	{"how is the cluster doing", "", ""},                             // vague
}

func TestRoutingCorpus(t *testing.T) {
	for _, c := range routingCorpus {
		pl, ok := Match(c.req)
		if c.kind == "" {
			if ok {
				t.Errorf("Match(%q) = %+v, want NO match", c.req, pl)
			}
			continue
		}
		if !ok {
			t.Errorf("Match(%q) = no match, want kind %q", c.req, c.kind)
			continue
		}
		if pl.Kind != c.kind {
			t.Errorf("Match(%q) kind = %q, want %q", c.req, pl.Kind, c.kind)
			continue
		}
		if c.app != "" {
			got := pl.App
			if pl.Kind == "list" {
				got = pl.Selector
			}
			if got != c.app {
				t.Errorf("Match(%q) entity = %q, want %q", c.req, got, c.app)
			}
		}
	}
}
