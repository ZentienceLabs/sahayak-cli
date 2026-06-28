package playbook

import (
	"strings"
	"testing"
)

func TestMatchListIntents(t *testing.T) {
	cases := []struct {
		req      string
		resource string
		selector string
	}{
		{"can you provide configmap list for acme-web", "configmaps", "acme-web"},
		{"list configmaps for acme-web", "configmaps", "acme-web"},
		{"show me the services in acme-dev", "services", "acme-dev"},
		{"get deployments for acme", "deployments", "acme"},
		{"list all secrets in kube-system", "secrets", "kube-system"},
		{"which pods of acme-worker are there", "pods", "acme-worker"},
	}
	for _, c := range cases {
		pl, ok := Match(c.req)
		if !ok {
			t.Fatalf("Match(%q) did not fire", c.req)
		}
		if pl.Resource != c.resource {
			t.Errorf("Match(%q) resource = %q, want %q", c.req, pl.Resource, c.resource)
		}
		if pl.Selector != c.selector {
			t.Errorf("Match(%q) selector = %q, want %q", c.req, pl.Selector, c.selector)
		}
	}
}

func TestMatchRejectsAmbiguousOrDiagnostic(t *testing.T) {
	// These must NOT fire — they need the adaptive model-driven loop, not a
	// deterministic single listing.
	reject := []string{
		"why did my pod crash in acme-dev",  // pod-level → investigate loop
		"are there any errors in acme dev",  // no hyphenated app to resolve
		"list failing pods across the cluster", // no explicit "for/in <entity>"
		"show me all namespaces",               // cluster-scoped, noise selector
		"configmap acme-web",                // no list verb, no preposition
		"list configmaps in the cluster",       // selector is noise ("cluster")
		"how is the cluster doing",             // no resource, no app
	}
	for _, r := range reject {
		if pl, ok := Match(r); ok {
			t.Errorf("Match(%q) fired unexpectedly: %+v", r, pl)
		}
	}
}

func TestMatchLogsIntents(t *testing.T) {
	cases := []struct {
		req   string
		app   string
		focus []string
	}{
		{"why is acme-web failing", "acme-web", nil},
		{"show me errors in acme-web logs", "acme-web", nil},
		{"acme-worker is crashing", "acme-worker", nil},
		{"debug the acme-web logs for github oauth", "acme-web", []string{"github", "oauth"}},
		{"why is github integration failing in acme-web", "acme-web", []string{"github", "integration"}},
	}
	for _, c := range cases {
		pl, ok := Match(c.req)
		if !ok || pl.Kind != "logs" {
			t.Fatalf("Match(%q) did not produce a logs plan: %+v ok=%v", c.req, pl, ok)
		}
		if pl.App != c.app {
			t.Errorf("Match(%q) app = %q, want %q", c.req, pl.App, c.app)
		}
		if !equalUnordered(pl.Focus, c.focus) {
			t.Errorf("Match(%q) focus = %v, want %v", c.req, pl.Focus, c.focus)
		}
	}
}

func TestMatchDeploymentIntents(t *testing.T) {
	cases := []struct {
		req    string
		kind   string
		app    string
		envVar string
	}{
		{"restart acme-web", "restart", "acme-web", ""},
		{"please redeploy acme-worker", "restart", "acme-worker", ""},
		{"what image is acme-web running", "image", "acme-web", ""},
		{"show the image tag for acme-ui", "image", "acme-ui", ""},
		{"rollout status of acme-web", "rollout", "acme-web", ""},
		{"is acme-worker rolled out", "rollout", "acme-worker", ""},
		{"is CONSOLE_WORKFLOW_REDESIGN set in acme-web", "verifyenv", "acme-web", "CONSOLE_WORKFLOW_REDESIGN"},
		{"verify FEATURE_X_ENABLED in acme-web", "verifyenv", "acme-web", "FEATURE_X_ENABLED"},
	}
	for _, c := range cases {
		pl, ok := Match(c.req)
		if !ok || pl.Kind != c.kind {
			t.Fatalf("Match(%q) kind = %q ok=%v, want %q", c.req, pl.Kind, ok, c.kind)
		}
		if pl.App != c.app {
			t.Errorf("Match(%q) app = %q, want %q", c.req, pl.App, c.app)
		}
		if pl.EnvVar != c.envVar {
			t.Errorf("Match(%q) envVar = %q, want %q", c.req, pl.EnvVar, c.envVar)
		}
	}
}

func TestMatchSearchConfig(t *testing.T) {
	cases := []struct {
		req, keyword string
	}{
		{"can you tell me if there is env variable in configmap related to workflow", "workflow"},
		{"find a config flag for telemetry", "telemetry"},
		{"which configmap key contains redis", "redis"},
		{"is there a setting about authentication", "authentication"},
		// looser phrasings with no explicit search word — carried by 2+ keywords:
		{"is there console workflow ui in configmap", "workflow"},
		{"do we have console workflow ui config", "workflow"},
		{"console redesign setting in configmap", "redesign"},
	}
	for _, c := range cases {
		pl, ok := Match(c.req)
		if !ok || pl.Kind != "searchcfg" {
			t.Fatalf("Match(%q) = %+v ok=%v, want searchcfg", c.req, pl, ok)
		}
		if pl.Keyword != c.keyword {
			t.Errorf("Match(%q) keyword = %q, want %q", c.req, pl.Keyword, c.keyword)
		}
	}
	// "list configmaps for acme-web" must stay a LIST, not a content search.
	if pl, _ := Match("list configmaps for acme-web"); pl.Kind != "list" {
		t.Errorf("list query mis-routed to %q", pl.Kind)
	}
}

func TestBuildPlanFillsSlots(t *testing.T) {
	cases := []struct {
		kind, req string
		check     func(Plan) bool
	}{
		{"list", "the configmaps for acme-web please", func(p Plan) bool { return p.Resource == "configmaps" && p.Selector == "acme-web" }},
		{"logs", "acme-worker keeps dying", func(p Plan) bool { return p.App == "acme-worker" }},
		{"image", "current tag of acme-web", func(p Plan) bool { return p.App == "acme-web" }},
		{"rollout", "acme-web status", func(p Plan) bool { return p.App == "acme-web" }},
		{"restart", "cycle acme-worker", func(p Plan) bool { return p.App == "acme-worker" }},
		{"verifyenv", "DEBUG_MODE in acme-web", func(p Plan) bool { return p.App == "acme-web" && p.EnvVar == "DEBUG_MODE" }},
		{"searchcfg", "anything about telemetry in there", func(p Plan) bool { return p.Keyword == "telemetry" }},
	}
	for _, c := range cases {
		pl, ok := BuildPlan(c.kind, c.req)
		if !ok {
			t.Errorf("BuildPlan(%q, %q) did not fire", c.kind, c.req)
			continue
		}
		if pl.Kind != c.kind {
			t.Errorf("BuildPlan(%q, ...) kind = %q", c.kind, pl.Kind)
		}
		if !c.check(pl) {
			t.Errorf("BuildPlan(%q, %q) slots wrong: %+v", c.kind, c.req, pl)
		}
	}
}

func TestBuildPlanDeclinesWhenSlotMissing(t *testing.T) {
	// A routed kind with no groundable slot must NOT produce a half-blind plan.
	for _, c := range []struct{ kind, req string }{
		{"logs", "why is it failing"},                   // no hyphenated app
		{"list", "show me everything"},                  // no resource, no selector
		{"verifyenv", "is the flag set in acme-web"}, // no UPPER_SNAKE env var
		{"searchcfg", "is it in there"},                 // no content keyword
	} {
		if pl, ok := BuildPlan(c.kind, c.req); ok {
			t.Errorf("BuildPlan(%q, %q) fired unexpectedly: %+v", c.kind, c.req, pl)
		}
	}
}

func TestVerifyEnvBacksOffOnAssignment(t *testing.T) {
	// "set FLAG to true" is a mutation, not a read — verifyenv must not fire.
	if pl, ok := Match("set CONSOLE_WORKFLOW_REDESIGN to true in acme-web"); ok && pl.Kind == "verifyenv" {
		t.Errorf("verifyenv fired on an assignment: %+v", pl)
	}
}

func TestRestartBeatsLogsWhenBothCouldMatch(t *testing.T) {
	// "restart acme-web because it is failing" has a diag trigger AND a restart
	// trigger — restart (the explicit action) must win.
	pl, ok := Match("restart acme-web because it is failing")
	if !ok || pl.Kind != "restart" {
		t.Fatalf("expected restart plan, got %+v ok=%v", pl, ok)
	}
}

func TestListBeatsLogsWhenBothCouldMatch(t *testing.T) {
	// "list failed pods for acme-web" has a diag trigger ("failed") AND a list
	// shape — the list intent must win (it's the explicit, higher-precision one).
	pl, ok := Match("list pods for acme-web")
	if !ok || pl.Kind != "list" {
		t.Fatalf("expected list plan, got %+v ok=%v", pl, ok)
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestFilterRowsMatchesNameAndNamespace(t *testing.T) {
	table := `NAMESPACE      NAME                  DATA   AGE
default        kube-root-ca.crt      1      88d
kube-system    coredns               1      88d
acme-dev    acme-web-config    7      40d
acme-demo   acme-web-config    7      22d
acme-demo   acme-web-favicon   1      22d
acme        acme-api-config    4      55d`

	rows := FilterRows(table, "acme-web")
	if len(rows) != 3 {
		t.Fatalf("FilterRows matched %d rows, want 3: %+v", len(rows), rows)
	}
	// Header and the unrelated coredns / acme-api rows must be excluded.
	for _, r := range rows {
		if r.Name == "coredns" || r.Name == "acme-api-config" || r.Name == "NAME" {
			t.Errorf("unexpected row matched: %+v", r)
		}
	}
}

func TestFilterRowsMatchesByNamespace(t *testing.T) {
	table := `NAMESPACE     NAME       DATA  AGE
acme-dev   web-cfg    2     1d
prod          web-cfg    2     1d`
	rows := FilterRows(table, "acme-dev")
	if len(rows) != 1 || rows[0].Namespace != "acme-dev" {
		t.Fatalf("namespace match failed: %+v", rows)
	}
}

func TestSummarizeGroupsNamespaces(t *testing.T) {
	rows := []Row{
		{Namespace: "acme-dev", Name: "acme-web-config"},
		{Namespace: "acme-demo", Name: "acme-web-config"},
		{Namespace: "acme-demo", Name: "acme-web-favicon"},
	}
	got := Summarize("configmaps", "acme-web", rows)
	if !strings.Contains(got, "Found 3 configmaps") {
		t.Errorf("missing count header: %q", got)
	}
	// The shared name should collapse to one line listing both namespaces.
	if !strings.Contains(got, "acme-web-config  (namespaces acme-demo, acme-dev)") {
		t.Errorf("did not group namespaces for shared name: %q", got)
	}
	if !strings.Contains(got, "acme-web-favicon  (namespace acme-demo)") {
		t.Errorf("singular namespace wording wrong: %q", got)
	}
}

func TestSummarizeTrueAbsence(t *testing.T) {
	got := Summarize("configmaps", "nope", nil)
	if !strings.Contains(got, "No configmaps matching \"nope\"") || !strings.Contains(got, "all namespaces") {
		t.Errorf("absence message wrong: %q", got)
	}
}
