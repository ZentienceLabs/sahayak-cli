package router

import (
	"context"
	"strings"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
	"github.com/ZentienceLabs/sahayak-cli/core/playbook"
)

// newTestRouter builds a router over the embedded catalog using the deterministic
// hash embedder, so these tests are hermetic (no network, no model).
func newTestRouter(t *testing.T) *Router {
	t.Helper()
	r, err := New(context.Background(), embed.NewHashEmbedder(256), "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRouteKnownPhrasings(t *testing.T) {
	r := newTestRouter(t)
	// Phrasings NOT verbatim in the catalog — the router should still land the right
	// kind by lexical/semantic overlap, then BuildPlan grounds the slot.
	cases := []struct {
		req  string
		kind string
	}{
		{"please list the configmaps for acme-web", "list"},
		{"why is acme-worker failing right now", "logs"},
		{"what image is acme-web running in prod", "image"},
		{"is acme-web rolled out yet", "rollout"},
		{"please restart acme-web", "restart"},
		{"is CONSOLE_WORKFLOW_REDESIGN set in acme-web", "verifyenv"},
		{"how is acme-web doing", "status"}, // composite intent, routed semantically
	}
	for _, c := range cases {
		m, ok, err := r.Route(context.Background(), c.req)
		if err != nil {
			t.Fatalf("Route(%q) error: %v", c.req, err)
		}
		if !ok {
			t.Errorf("Route(%q) did not fire (best below threshold)", c.req)
			continue
		}
		if m.Plan.Kind != c.kind {
			t.Errorf("Route(%q) kind = %q (via %q @ %.2f), want %q", c.req, m.Plan.Kind, m.Intent, m.Score, c.kind)
		}
	}
}

func TestRouterExtendsRegexCoverage(t *testing.T) {
	// The whole point: phrasings the deterministic regex Match() does NOT catch, the
	// semantic router DOES — and grounds the same plan. If regex already fired, that's
	// fine too (it runs first in the pipeline); this asserts the router covers the gap.
	r := newTestRouter(t)
	cases := []struct {
		req  string
		kind string
	}{
		{"what's going wrong with acme-web", "logs"},  // "wrong" isn't a regex diag trigger
		{"which version is acme-web on", "image"},     // no "image/tag" keyword
		{"cycle the pods for acme-worker", "restart"}, // no restart/redeploy/bounce token
	}
	for _, c := range cases {
		if _, regexFired := playbook.Match(c.req); regexFired {
			// Not a failure, but this case no longer demonstrates the gap — note it.
			t.Logf("note: regex Match already fires for %q; router still verified below", c.req)
		}
		m, ok, err := r.Route(context.Background(), c.req)
		if err != nil {
			t.Fatalf("Route(%q) error: %v", c.req, err)
		}
		if !ok || m.Plan.Kind != c.kind {
			t.Errorf("router did not cover the gap for %q: ok=%v kind=%q want %q", c.req, ok, m.Plan.Kind, c.kind)
		}
	}
}

func TestRouteDeclinesOffTopic(t *testing.T) {
	r := newTestRouter(t)
	// Clearly unrelated requests must NOT be force-fit to a k8s intent.
	for _, req := range []string{
		"what is the capital of France",
		"write me a haiku about the sea",
		"is go installed on this machine",
	} {
		if m, ok, _ := r.Route(context.Background(), req); ok {
			t.Errorf("Route(%q) fired unexpectedly: %+v", req, m)
		}
	}
}

func TestRouteDeclinesWhenSlotMissing(t *testing.T) {
	r := newTestRouter(t)
	// "why is it failing" matches the logs intent by meaning, but there is no
	// resolvable hyphenated app, so BuildPlan declines and the router must too.
	if m, ok, _ := r.Route(context.Background(), "why is it failing"); ok {
		t.Errorf("Route fired without a groundable app: %+v", m)
	}
}

func TestParseCatalogRejectsBadKind(t *testing.T) {
	_, err := parseCatalog("intent foo notakind\n- example phrase")
	if err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("expected unknown-kind error, got %v", err)
	}
}

func TestParseCatalogRejectsOrphanExample(t *testing.T) {
	if _, err := parseCatalog("- an example with no intent above it"); err == nil {
		t.Fatal("expected orphan-example error, got nil")
	}
}

func TestExtraCatalogAppends(t *testing.T) {
	extra := "intent search-config searchcfg\n- got that special sauce thingy configured"
	r, err := New(context.Background(), embed.NewHashEmbedder(256), extra)
	if err != nil {
		t.Fatalf("New with extra: %v", err)
	}
	// The extra phrase should be embedded and routable.
	m, ok, err := r.Route(context.Background(), "is the special sauce thingy configured in configmap")
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if !ok || m.Plan.Kind != "searchcfg" {
		t.Errorf("extra catalog phrase not routed to searchcfg: ok=%v %+v", ok, m)
	}
}
