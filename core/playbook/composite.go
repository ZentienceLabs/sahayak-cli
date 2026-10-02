package playbook

import "strings"

// Composite is a higher-level intent that runs SEVERAL atomic playbooks and combines
// their results into one answer — the deterministic composition layer. Where an atomic
// Plan answers a single fact ("what image is X on"), a Composite answers a question that
// only makes sense as a synthesis of several facts ("how is X doing" = image + rollout +
// recent errors). The division of labor is unchanged: a matcher (or, later, the model)
// chooses WHICH composite; Go runs the parts and synthesizes the verdict. The model
// never authors a command.
type Composite struct {
	Kind  string   // e.g. "status"
	App   string   // resolved app/deployment keyword, e.g. "acme-web"
	Parts []string // the atomic playbooks composed, in order, e.g. ["image","rollout","logs"]
}

// statusTriggers are rollup phrasings — asking for an overall health picture, not a
// single fact. They deliberately EXCLUDE the bare word "status" (which belongs to the
// atomic rollout play, e.g. "rollout status of X") and the diagnostic triggers that
// route to "logs" (error/failing/crash/why — a targeted error hunt). Composite is
// tried only AFTER the atomic matchers miss, so these never steal an atomic intent.
var statusTriggers = []string{
	"how is", "how's", "how are things", "how are we doing", "how is it going with",
	"health of", "health check", "healthy", "is everything ok", "everything ok with",
	"rundown", "summary of", "summarize", "overall", "state of", "give me a status on",
	"how healthy",
}

// MatchComposite recognizes a status-rollup request for a single app and returns the
// composite (image + rollout + recent-error scan). It requires a hyphenated app token
// to ground the target, like the deployment-targeted atomic matchers. Returns ok=false
// for anything else, so the caller falls through to the router / classifier / loop.
func MatchComposite(request string) (Composite, bool) {
	r := strings.ToLower(request)
	hit := false
	for _, t := range statusTriggers {
		if strings.Contains(r, t) {
			hit = true
			break
		}
	}
	if !hit {
		return Composite{}, false
	}
	app := appEntity(strings.Fields(r))
	if !looksLikeApp(app) {
		return Composite{}, false
	}
	return Composite{Kind: "status", App: app, Parts: []string{"image", "rollout", "logs"}}, true
}
