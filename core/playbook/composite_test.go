package playbook

import "testing"

func TestMatchCompositeFires(t *testing.T) {
	cases := []struct {
		req string
		app string
	}{
		{"how is acme-web doing", "acme-web"},
		{"how's acme-worker", "acme-worker"},
		{"health of acme-web", "acme-web"},
		{"is acme-web healthy", "acme-web"},
		{"give me a rundown of acme-worker", "acme-worker"},
		{"summarize acme-web", "acme-web"},
		{"what's the overall state of acme-web", "acme-web"},
	}
	for _, c := range cases {
		got, ok := MatchComposite(c.req)
		if !ok {
			t.Errorf("MatchComposite(%q) did not fire", c.req)
			continue
		}
		if got.Kind != "status" || got.App != c.app {
			t.Errorf("MatchComposite(%q) = %+v, want status/%s", c.req, got, c.app)
		}
		if len(got.Parts) == 0 {
			t.Errorf("MatchComposite(%q) has no parts", c.req)
		}
	}
}

func TestMatchCompositeDeclines(t *testing.T) {
	// These must NOT be composites — either an atomic single-fact intent, or
	// ungroundable. Composite must leave them to the atomic matchers / loop.
	reject := []string{
		"rollout status of acme-web",     // atomic rollout — "status" alone is not a rollup trigger
		"why is acme-web failing",        // atomic logs
		"what image is acme-web running", // atomic image
		"list configmaps for acme-web",   // atomic list
		"how is the cluster doing",       // no hyphenated app to ground
		"summarize the situation",        // no app
		"restart acme-web",               // atomic restart
	}
	for _, r := range reject {
		if got, ok := MatchComposite(r); ok {
			t.Errorf("MatchComposite(%q) fired unexpectedly: %+v", r, got)
		}
	}
}

// TestCompositeDoesNotStealAtomicIntents guards the precedence contract: every phrasing
// the atomic Match owns must still be owned by Match (the agent tries Match first, but
// this proves the two matchers don't both claim the same request).
func TestCompositeDoesNotStealAtomicIntents(t *testing.T) {
	atomic := []string{
		"rollout status of acme-web",
		"what image is acme-web running",
		"restart acme-web",
		"why is acme-web failing",
	}
	for _, r := range atomic {
		if _, ok := Match(r); !ok {
			t.Errorf("precondition: atomic Match(%q) should fire", r)
		}
		if _, ok := MatchComposite(r); ok {
			t.Errorf("MatchComposite(%q) also fired — overlaps an atomic intent", r)
		}
	}
}
