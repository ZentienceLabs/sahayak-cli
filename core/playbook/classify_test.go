package playbook

import "testing"

func TestFromClassificationValid(t *testing.T) {
	cases := []struct {
		req, intent, app, resource, env string
		wantKind                        string
	}{
		{"any errors in the acme-web app", "logs", "acme-web", "", "", "logs"},
		{"configmaps for acme-web please", "list", "acme-web", "configmap", "", "list"},
		{"the image of acme-web", "image", "acme-web", "", "", "image"},
		{"restart acme-web now", "restart", "acme-web", "", "", "restart"},
		{"is DEBUG_MODE set in acme-web", "verifyenv", "acme-web", "", "DEBUG_MODE", "verifyenv"},
	}
	for _, c := range cases {
		pl, ok := FromClassification(c.req, c.intent, c.app, c.resource, c.env)
		if !ok || pl.Kind != c.wantKind {
			t.Errorf("FromClassification(%q,%q,%q) = %+v ok=%v, want kind %q", c.req, c.intent, c.app, pl, ok, c.wantKind)
		}
	}
}

func TestFromClassificationRejectsPhraseAndBarePrefixApp(t *testing.T) {
	// "acme dev" (a two-word environment phrase) and "acme" (a bare prefix that
	// would match every deployment) must NOT route to a deployment playbook — they
	// belong in the adaptive loop.
	if _, ok := FromClassification("are there errors in the acme dev environment", "logs", "acme dev", "", ""); ok {
		t.Error("accepted a multi-word phrase as an app")
	}
	if _, ok := FromClassification("anything wrong in acme", "logs", "acme", "", ""); ok {
		t.Error("accepted a bare non-hyphenated prefix as an app")
	}
}

func TestFromClassificationRejectsHallucinatedApp(t *testing.T) {
	// The model named an app that does NOT appear in the request — must be rejected.
	if _, ok := FromClassification("why is the website down", "logs", "acme-web", "", ""); ok {
		t.Error("accepted an app name not grounded in the request")
	}
}

func TestFromClassificationRejectsUnknownIntentAndBadResource(t *testing.T) {
	if _, ok := FromClassification("do the thing for acme-web", "frobnicate", "acme-web", "", ""); ok {
		t.Error("accepted an unknown intent")
	}
	if _, ok := FromClassification("get widgets for acme-web", "list", "acme-web", "widgets", ""); ok {
		t.Error("accepted an unknown resource for list")
	}
	if _, ok := FromClassification("none of this", "none", "", "", ""); ok {
		t.Error("accepted 'none'")
	}
}

func TestFromClassificationVerifyEnvRequiresGroundedVar(t *testing.T) {
	// env_var must also appear in the request.
	if _, ok := FromClassification("check env in acme-web", "verifyenv", "acme-web", "", "MADE_UP_VAR"); ok {
		t.Error("accepted an env var not grounded in the request")
	}
}

func TestFromClassificationVerifyEnvRejectsNonEnvShape(t *testing.T) {
	// A model that mis-routes "show acme-web configmaps" to verifyenv with
	// env_var="configmaps" (a grounded but lowercase, non-env word) must be rejected.
	if pl, ok := FromClassification("show acme-web configmaps", "verifyenv", "acme-web", "", "configmaps"); ok {
		t.Errorf("accepted a non-env-shaped env var: %+v", pl)
	}
}
