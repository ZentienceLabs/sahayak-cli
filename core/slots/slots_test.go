package slots

import "testing"

func TestExtractPrimitives(t *testing.T) {
	resourceVals := map[string]string{
		"configmap": "configmaps", "configmaps": "configmaps", "cm": "configmaps",
		"service": "services", "svc": "services", "pods": "pods", "pod": "pods",
	}
	cases := []struct {
		name    string
		spec    Spec
		request string
		want    string
		wantOK  bool
	}{
		{"app", Spec{Extractor: "hyphenated-token"}, "why is acme-web failing", "acme-web", true},
		{"app-none", Spec{Extractor: "hyphenated-token"}, "how is the cluster", "", false},
		{"env", Spec{Extractor: "upper-snake"}, "is CONSOLE_WORKFLOW_REDESIGN set in acme-web", "CONSOLE_WORKFLOW_REDESIGN", true},
		{"env-skips-acronym", Spec{Extractor: "upper-snake"}, "is the AKS url ok", "", false},
		{"selector", Spec{Extractor: "after-preposition"}, "list the configmaps for acme-web", "acme-web", true},
		{"selector-fallback", Spec{Extractor: "after-preposition"}, "acme-web configmaps", "acme-web", true},
		{"keyword", Spec{Extractor: "content-keyword"}, "is there a config flag for telemetry", "telemetry", true},
		{"enum", Spec{Extractor: "enum", Values: resourceVals}, "show me the cm for acme-web", "configmaps", true},
		{"enum-canonicalizes", Spec{Extractor: "enum", Values: resourceVals}, "list svc in acme-dev", "services", true},
		{"enum-none", Spec{Extractor: "enum", Values: resourceVals}, "restart acme-web", "", false},
		{"after-verb", Spec{Extractor: "after-verb", Verbs: []string{"restart", "stop", "start"}}, "restart nginx", "nginx", true},
		{"after-verb-skips-stopword", Spec{Extractor: "after-verb", Verbs: []string{"restart"}}, "restart the nginx service", "nginx", true},
		{"after-verb-none", Spec{Extractor: "after-verb", Verbs: []string{"restart"}}, "how is nginx", "", false},
	}
	for _, c := range cases {
		got, ok := Extract(c.spec, c.request)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: Extract(%q) = (%q,%v), want (%q,%v)", c.name, c.request, got, ok, c.want, c.wantOK)
		}
	}
}

func TestExtractAll(t *testing.T) {
	specs := []Spec{
		{Name: "resource", Extractor: "enum", Values: map[string]string{"configmaps": "configmaps"}, Required: true},
		{Name: "selector", Extractor: "after-preposition", Required: true},
	}
	got, ok := ExtractAll(specs, "list the configmaps for acme-web")
	if !ok {
		t.Fatal("ExtractAll should ground both required slots")
	}
	if got["resource"] != "configmaps" || got["selector"] != "acme-web" {
		t.Errorf("ExtractAll = %+v", got)
	}

	// A missing required slot must fail the whole extraction.
	if _, ok := ExtractAll(specs, "show me everything"); ok {
		t.Error("ExtractAll should fail when a required slot is missing")
	}
}

// TestUnknownExtractorDeclines guards against a typo'd cartridge silently grounding.
func TestUnknownExtractorDeclines(t *testing.T) {
	if _, ok := Extract(Spec{Extractor: "nonsense"}, "acme-web"); ok {
		t.Error("unknown extractor must return ok=false")
	}
}
