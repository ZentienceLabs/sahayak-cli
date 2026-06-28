package playbook

import (
	"regexp"
	"strings"
)

// envVarShapeRe guards the classifier's env_var: it must LOOK like an environment
// variable (uppercase, underscores), not just be any grounded word. Without this a
// model that mis-reads "show configmaps" as verifyenv with env_var="configmaps"
// would slip through grounding (the word IS in the request) and run `printenv
// configmaps`.
var envVarShapeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,}$`)

// FromClassification builds a Plan from a model's intent classification, but only
// after DETERMINISTIC validation — the model is trusted to *route*, never to invent.
// In particular the extracted entity must actually appear in the original request
// (so a small model cannot hallucinate a deployment name), the intent must be known,
// and per-intent required fields must resolve. Returns ok=false to fall through to
// the adaptive loop whenever anything is off.
//
// This is the "tiny intent-classifier" fallback: when the regex matchers miss, one
// small model call yields {intent, app, resource, env_var}, and this turns it into a
// real Plan — widening playbook coverage without trusting the model to plan.
func FromClassification(request, intent, app, resource, envVar string) (Plan, bool) {
	intent = strings.ToLower(strings.TrimSpace(intent))
	app = strings.TrimSpace(app)

	switch intent {
	case "list":
		// The selector is a single grounded token (an app name or a namespace), not a
		// phrase — "acme dev" must not slip through as one selector.
		res, ok := resourceAlias[strings.ToLower(strings.TrimSpace(resource))]
		if !ok || !groundedToken(request, app) {
			return Plan{}, false
		}
		return Plan{Kind: "list", Resource: res, Display: res, Selector: app}, true

	case "logs", "image", "rollout", "restart":
		// These resolve to a real deployment, so the app must look like one: a single
		// hyphenated token (acme-web), not a phrase ("acme dev") or a bare prefix
		// ("acme") that would match everything. This mirrors the deterministic
		// appEntity rule and keeps the classifier from routing vague asks to a wrong
		// "no deployment matching X" conclusion.
		if !looksLikeApp(app) || !groundedToken(request, app) {
			return Plan{}, false
		}
		return Plan{Kind: intent, App: app}, true

	case "verifyenv":
		envVar = strings.TrimSpace(envVar)
		if !envVarShapeRe.MatchString(envVar) || !looksLikeApp(app) ||
			!groundedToken(request, app) || !grounded(request, envVar) {
			return Plan{}, false
		}
		return Plan{Kind: "verifyenv", App: app, EnvVar: envVar}, true

	default:
		return Plan{}, false // "none" or anything unrecognized
	}
}

// looksLikeApp reports whether the classifier's app is shaped like a real deployment:
// a single hyphenated token (acme-web), not a phrase or a bare prefix.
func looksLikeApp(app string) bool {
	app = strings.TrimSpace(app)
	return len(app) >= 4 && !strings.ContainsAny(app, " \t") && strings.Contains(app, "-")
}

// groundedToken is grounded() plus "must be a single token" — rejects multi-word
// phrases the model might echo as one entity ("acme dev").
func groundedToken(request, value string) bool {
	value = strings.TrimSpace(value)
	return !strings.ContainsAny(value, " \t") && grounded(request, value)
}

// MightBeK8s is a cheap deterministic gate for the classifier fallback: it reports
// whether a request plausibly targets a Kubernetes workload at all (a hyphenated app
// name, a known resource noun, a diagnostic trigger, an action trigger, or an env
// var). Requests with none of these — "is the go toolchain available", "what time is
// it" — skip the classifier entirely, so we never spend a model call routing
// something that obviously isn't a k8s playbook.
func MightBeK8s(request string) bool {
	if envVarRe.MatchString(request) {
		return true
	}
	for _, t := range tokenize(request) {
		switch {
		case resourceAlias[t] != "", diagTriggers[t], restartTriggers[t], imageTriggers[t]:
			return true
		case t == "rollout" || t == "rolled":
			return true
		case appLike(t) && strings.Contains(t, "-"):
			return true
		}
	}
	return false
}

// grounded reports whether value actually appears in the request (case-insensitive).
// This is the anti-hallucination guard: the classifier may only name things the
// operator actually typed.
func grounded(request, value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 {
		return false
	}
	return strings.Contains(strings.ToLower(request), strings.ToLower(value))
}
