// Package playbook recognizes a small set of high-frequency, unambiguous DevOps
// intents and turns them into a DETERMINISTIC plan, so the agent does not have to
// rely on a small local model to plan AND to know when to stop. The model's job (if
// any) shrinks to the two things a 4B is reliable at — classifying the intent and
// naming the entity — while Go does the listing, filtering, and conclusion.
//
// This is the forward version of the crossCheckAbsence guard: instead of correcting
// a wrong "not found" after the fact, the playbook scans all namespaces up front and
// concludes from the data, so the loop-stopping failure cannot occur.
package playbook

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Plan is a recognized, fully-parameterized intent.
//
//	Kind=="list":      list a resource and filter it      -> Resource/Display/Selector
//	Kind=="logs":      find a deployment, scan its logs    -> App/Focus
//	Kind=="image":     report a deployment's container image-> App
//	Kind=="rollout":   report a deployment's rollout status -> App
//	Kind=="restart":   restart a deployment (MUTATING)     -> App
//	Kind=="verifyenv": print an env var from a deployment  -> App/EnvVar
type Plan struct {
	Kind string // see above

	// list fields
	Resource string // canonical kubectl resource, e.g. "configmaps"
	Display  string // human label, e.g. "configmaps"
	Selector string // app/name/namespace keyword to match, e.g. "acme-web"

	// deployment-targeted fields (logs/image/rollout/restart/verifyenv)
	App    string   // app/deployment keyword to resolve, e.g. "acme-web"
	Focus  []string // logs: optional subsystem keywords, e.g. ["github","oauth"]
	EnvVar string   // verifyenv: the env var to read, e.g. "CONSOLE_WORKFLOW_REDESIGN"

	// searchcfg field
	Keyword string // searchcfg: substring to find in configmap keys/values, e.g. "workflow"
}

// resourceAlias maps the nouns an operator might type to the canonical kubectl
// resource word. Only NAMESPACED resources are listed: the playbook lists with
// "-A", which is invalid for cluster-scoped kinds (namespaces, nodes), so those are
// deliberately excluded and fall through to the normal investigate loop.
var resourceAlias = map[string]string{
	"configmap": "configmaps", "configmaps": "configmaps", "cm": "configmaps",
	"service": "services", "services": "services", "svc": "services",
	"deployment": "deployments", "deployments": "deployments", "deploy": "deployments",
	"pod": "pods", "pods": "pods", "po": "pods",
	"secret": "secrets", "secrets": "secrets",
	"ingress": "ingress", "ingresses": "ingress", "ing": "ingress",
	"statefulset": "statefulsets", "statefulsets": "statefulsets", "sts": "statefulsets",
	"daemonset": "daemonsets", "daemonsets": "daemonsets", "ds": "daemonsets",
	"replicaset": "replicasets", "replicasets": "replicasets", "rs": "replicasets",
	"job": "jobs", "jobs": "jobs",
	"cronjob": "cronjobs", "cronjobs": "cronjobs",
	"endpoint": "endpoints", "endpoints": "endpoints", "ep": "endpoints",
	"pvc": "persistentvolumeclaims", "persistentvolumeclaim": "persistentvolumeclaims",
	"persistentvolumeclaims": "persistentvolumeclaims",
}

// listVerbs gate the intent: we only fire on an explicit "list/show me the…" style
// request, never on a diagnostic one ("why did X crash", "errors in Y").
var listVerbs = map[string]bool{
	"list": true, "show": true, "get": true, "provide": true, "display": true,
	"give": true, "fetch": true, "find": true, "see": true, "what": true,
	"which": true, "all": true, "enumerate": true,
}

// selectorPreps introduce the entity to match ("…for acme-web", "…in acme-dev").
// We require an explicit preposition rather than guessing the entity from a stray
// token, so the playbook only fires when the target is unambiguous.
var selectorPreps = map[string]bool{
	"for": true, "in": true, "of": true, "named": true,
	"called": true, "matching": true, "within": true,
}

// selectorNoise are tokens that, even after a preposition, are not a real entity
// ("…in the cluster", "…for all namespaces") — seeing one means "no clear target".
var selectorNoise = map[string]bool{
	"the": true, "a": true, "an": true, "my": true, "your": true, "this": true,
	"that": true, "all": true, "every": true, "any": true, "cluster": true,
	"namespace": true, "namespaces": true, "ns": true, "them": true, "it": true,
}

// diagTriggers signal a "something is wrong, look at the logs" request — the §A
// pattern from the debug runbook. Their presence routes to the logs playbook.
var diagTriggers = map[string]bool{
	"error": true, "errors": true, "erroring": true, "failing": true, "failed": true,
	"fail": true, "fails": true, "crash": true, "crashing": true, "crashed": true,
	"broken": true, "logs": true, "log": true, "why": true, "debug": true,
	"exception": true, "exceptions": true, "issue": true, "issues": true,
	"throwing": true, "stacktrace": true, "traceback": true, "500": true, "503": true,
}

// connectives are filler words that are never an app name or a focus keyword.
var connectives = map[string]bool{
	"is": true, "are": true, "was": true, "were": true, "has": true, "have": true,
	"does": true, "did": true, "the": true, "this": true, "that": true, "with": true,
	"from": true, "there": true, "any": true, "some": true, "what": true, "when": true,
	"where": true, "please": true, "can": true, "you": true, "could": true, "would": true,
	"tell": true, "show": true, "find": true, "get": true, "give": true, "and": true,
	"running": true, "happening": true, "going": true, "app": true, "application": true,
}

// Match parses a request into a Plan, or returns ok=false to fall through to the
// adaptive loop. Matchers run most-specific first so a more explicit intent wins
// over a broader one (e.g. "restart X because failing" → restart, not logs).
// Anything ambiguous is left to the model-driven investigation.
func Match(request string) (Plan, bool) {
	toks := tokenize(request)
	if len(toks) == 0 {
		return Plan{}, false
	}
	for _, m := range []func(string, []string) (Plan, bool){
		matchList,         // "list configmaps for X"
		matchVerifyEnv,    // "is CONSOLE_X set in X"
		matchSearchConfig, // "is there a config/env var related to X"
		matchRestart,      // "restart X"          (mutating)
		matchImage,        // "what image is X running"
		matchRollout,      // "rollout status of X"
		matchLogs,         // "why is X failing"   (broadest diagnostic)
	} {
		if pl, ok := m(request, toks); ok {
			return pl, true
		}
	}
	return Plan{}, false
}

// BuildPlan fills the slots for an already-decided intent KIND, reusing the exact
// same deterministic extractors as the regex matchers. It returns ok=false when a
// required slot is absent (e.g. a "logs" intent with no resolvable app), so a caller
// that decided the kind some other way — the semantic Router — can only fire when Go
// can ground every slot the kind needs. This keeps ALL slot logic in one place,
// shared by Match (regex) and the Router (semantic), so the two can never disagree on
// how an app/env-var/keyword is extracted.
func BuildPlan(kind, request string) (Plan, bool) {
	toks := tokenize(request)
	if len(toks) == 0 {
		return Plan{}, false
	}
	switch kind {
	case "list":
		resource, display := "", ""
		for _, t := range toks {
			if r, ok := resourceAlias[t]; ok {
				resource, display = r, t
				break
			}
		}
		if resource == "" {
			return Plan{}, false
		}
		sel := selectorEntity(toks)
		if sel == "" {
			return Plan{}, false
		}
		if display == "" || resourceAlias[display] != "" {
			display = resource
		}
		return Plan{Kind: "list", Resource: resource, Display: display, Selector: sel}, true
	case "logs":
		app := appEntity(toks)
		if app == "" {
			return Plan{}, false
		}
		return Plan{Kind: "logs", App: app, Focus: focusKeywords(toks, app)}, true
	case "image", "rollout", "restart", "status":
		// "status" is the composition kind (a status rollup): the router can route to it
		// by meaning, and runPlan expands it into the multi-play Composite. Like the other
		// deployment-targeted kinds it just needs a grounded app.
		app := appEntity(toks)
		if app == "" {
			return Plan{}, false
		}
		return Plan{Kind: kind, App: app}, true
	case "verifyenv":
		envVar := envVarRe.FindString(request)
		app := appEntity(toks)
		if envVar == "" || app == "" {
			return Plan{}, false
		}
		return Plan{Kind: "verifyenv", App: app, EnvVar: envVar}, true
	case "searchcfg":
		var kws []string
		for _, t := range toks {
			if isContentKeyword(t) {
				kws = append(kws, t)
			}
		}
		if len(kws) == 0 {
			return Plan{}, false
		}
		return Plan{Kind: "searchcfg", Keyword: longest(kws)}, true
	}
	return Plan{}, false
}

// selectorEntity extracts the list selector: the entity after a selector preposition
// ("…for acme-web") that isn't noise/a resource/another preposition, falling back
// to the first hyphenated app-like token if no preposition framed it.
func selectorEntity(toks []string) string {
	sel := ""
	for i := 0; i < len(toks)-1; i++ {
		if selectorPreps[toks[i]] {
			cand := toks[i+1]
			if len(cand) >= 3 && !selectorNoise[cand] && resourceAlias[cand] == "" && !selectorPreps[cand] {
				sel = cand // last explicit preposition wins
			}
		}
	}
	if sel == "" {
		sel = appEntity(toks)
	}
	return sel
}

// matchList recognizes "list <resource> for/in <entity>". HIGH-PRECISION: requires a
// known namespaced resource noun, a listing verb, and an explicit non-noise entity
// after a preposition.
func matchList(_ string, toks []string) (Plan, bool) {
	resource, resIdx, display := "", -1, ""
	hasVerb := false
	for i, t := range toks {
		if listVerbs[t] {
			hasVerb = true
		}
		if r, ok := resourceAlias[t]; ok && resIdx == -1 {
			resource, resIdx, display = r, i, t
		}
	}
	if resIdx == -1 || !hasVerb {
		return Plan{}, false
	}
	selector := ""
	for i := 0; i < len(toks)-1; i++ {
		if selectorPreps[toks[i]] {
			cand := toks[i+1]
			if len(cand) >= 3 && !selectorNoise[cand] && resourceAlias[cand] == "" && !selectorPreps[cand] {
				selector = cand // last explicit preposition wins
			}
		}
	}
	if selector == "" {
		return Plan{}, false
	}
	if display == "" || resourceAlias[display] != "" {
		display = resource
	}
	return Plan{Kind: "list", Resource: resource, Display: display, Selector: selector}, true
}

// matchLogs recognizes "why is <app> failing", "errors in <app> logs", "<app> logs".
// HIGH-PRECISION on purpose: it fires only with a diagnostic trigger AND a hyphenated
// app name (every real deployment here looks like "acme-web"). Two deliberate
// hand-offs to the adaptive loop: pod-level queries ("why did my POD crash") go to the
// loop's pod-health path (which knows --previous), and bare single-word entities are
// too ambiguous to resolve deterministically.
func matchLogs(_ string, toks []string) (Plan, bool) {
	hasDiag := false
	for _, t := range toks {
		if diagTriggers[t] {
			hasDiag = true
		}
		if t == "pod" || t == "pods" || t == "po" {
			return Plan{}, false // pod-level diagnosis belongs to the investigate loop
		}
	}
	if !hasDiag {
		return Plan{}, false
	}
	app := appEntity(toks)
	if app == "" {
		return Plan{}, false
	}
	return Plan{Kind: "logs", App: app, Focus: focusKeywords(toks, app)}, true
}

// envVarRe matches a shell-style environment variable name: an uppercase token with
// at least one underscore (so it doesn't catch acronyms like "AKS" or "URL"). Read
// from the ORIGINAL request because tokenize() lowercases.
var envVarRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b`)

// mutationVerbs before the variable signal an assignment ("set FLAG …", "export
// FLAG …"). After the variable, "set" means "configured" ("is FLAG set"), a read.
var mutationVerbs = map[string]bool{
	"set": true, "export": true, "change": true, "update": true, "edit": true, "unset": true,
}

// matchVerifyEnv recognizes "is CONSOLE_WORKFLOW_REDESIGN set in acme-web" / "verify
// FLAG in <app>": an ENV_VAR token + a resolvable app, and NOT an assignment. The
// assignment check is POSITIONAL — a mutation verb before the var, or "to" after it
// ("set FLAG to true"), backs off; "is FLAG set" (verb after the var) is a read.
func matchVerifyEnv(request string, toks []string) (Plan, bool) {
	envVar := envVarRe.FindString(request)
	if envVar == "" {
		return Plan{}, false
	}
	envIdx := indexOf(toks, strings.ToLower(envVar))
	for i, t := range toks {
		if mutationVerbs[t] && (envIdx < 0 || i < envIdx) {
			return Plan{}, false
		}
		if t == "to" && envIdx >= 0 && i > envIdx {
			return Plan{}, false
		}
	}
	app := appEntity(toks)
	if app == "" {
		return Plan{}, false
	}
	return Plan{Kind: "verifyenv", App: app, EnvVar: envVar}, true
}

func indexOf(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

// configWords and searchWords gate the config-search intent: it fires only when the
// request mentions config/env AND asks to find/relate something — so "find an env var
// related to workflow", not "list configmaps for acme-web" (which is matchList).
var configWords = map[string]bool{
	"config": true, "configs": true, "configmap": true, "configmaps": true, "cm": true,
	"setting": true, "settings": true, "env": true, "environment": true,
}
var searchWords = map[string]bool{
	"variable": true, "variables": true, "var": true, "vars": true, "flag": true,
	"flags": true, "key": true, "keys": true, "value": true, "values": true,
	"related": true, "relating": true, "contains": true, "containing": true,
	"about": true, "matching": true, "search": true, "find": true, "having": true,
}

// cfgMutationVerbs mean the operator wants to CHANGE configmaps, not search them —
// hand those to the loop/approval gate, not the read-only search.
var cfgMutationVerbs = map[string]bool{
	"edit": true, "delete": true, "create": true, "apply": true, "patch": true,
	"remove": true, "update": true, "change": true, "add": true, "set": true,
}

// matchSearchConfig recognizes "is there a config key / env var related to <keyword>"
// AND looser phrasings like "console workflow ui in configmap". It searches configmap
// CONTENTS (keys+values) for a keyword — the grep-over-config task the model botches
// with jsonpath. Fires when a config word is present AND the intent is clearly a
// search: either an explicit search word ("related", "flag", "variable", …) OR at
// least TWO distinctive content keywords (e.g. "console" + "workflow"). Mutation
// requests back off.
func matchSearchConfig(_ string, toks []string) (Plan, bool) {
	hasConfig, hasSearch := false, false
	var kws []string
	for _, t := range toks {
		if configWords[t] {
			hasConfig = true
		}
		if searchWords[t] {
			hasSearch = true
		}
		if cfgMutationVerbs[t] {
			return Plan{}, false
		}
		if isContentKeyword(t) {
			kws = append(kws, t)
		}
	}
	if !hasConfig || len(kws) == 0 {
		return Plan{}, false
	}
	if !hasSearch && len(kws) < 2 {
		return Plan{}, false // a config word + one bare noun is too weak to be a search
	}
	return Plan{Kind: "searchcfg", Keyword: longest(kws)}, true
}

// isContentKeyword reports whether a token is a distinctive thing to search FOR (not a
// trigger/filler/resource word) — e.g. "workflow", "console", "telemetry".
func isContentKeyword(t string) bool {
	return len(t) >= 4 && !configWords[t] && !searchWords[t] && !connectives[t] &&
		!selectorPreps[t] && !selectorNoise[t] && !listVerbs[t] && !diagTriggers[t] &&
		resourceAlias[t] == ""
}

func longest(ss []string) string {
	best := ""
	for _, s := range ss {
		if len(s) > len(best) {
			best = s
		}
	}
	return best
}

// restartTriggers route "restart/redeploy/bounce <app>" to the mutating restart play.
var restartTriggers = map[string]bool{"restart": true, "redeploy": true, "bounce": true}

func matchRestart(_ string, toks []string) (Plan, bool) {
	has := false
	for _, t := range toks {
		if restartTriggers[t] {
			has = true
			break
		}
	}
	if !has {
		return Plan{}, false
	}
	app := appEntity(toks)
	if app == "" {
		return Plan{}, false
	}
	return Plan{Kind: "restart", App: app}, true
}

// imageTriggers route "what image/tag is <app> running" to the image play.
var imageTriggers = map[string]bool{"image": true, "images": true, "tag": true}

func matchImage(_ string, toks []string) (Plan, bool) {
	has := false
	for _, t := range toks {
		if imageTriggers[t] {
			has = true
			break
		}
	}
	if !has {
		return Plan{}, false
	}
	app := appEntity(toks)
	if app == "" {
		return Plan{}, false
	}
	return Plan{Kind: "image", App: app}, true
}

// matchRollout routes "rollout status of <app>" / "is <app> rolled out" to the rollout
// play. "rollout restart" is caught earlier by matchRestart, so this is status-only.
func matchRollout(_ string, toks []string) (Plan, bool) {
	has := false
	for i, t := range toks {
		if t == "rollout" || (t == "rolled" && i+1 < len(toks) && toks[i+1] == "out") {
			has = true
			break
		}
	}
	if !has {
		return Plan{}, false
	}
	app := appEntity(toks)
	if app == "" {
		return Plan{}, false
	}
	return Plan{Kind: "rollout", App: app}, true
}

// appLike reports whether a token could be an app/deployment name (not filler, a
// verb, a preposition, a resource noun, or a diagnostic trigger).
func appLike(t string) bool {
	if len(t) < 4 {
		return false
	}
	return !selectorNoise[t] && !selectorPreps[t] && !diagTriggers[t] &&
		!listVerbs[t] && !connectives[t] && resourceAlias[t] == ""
}

// appEntity picks the target app: the first hyphenated, app-like token (real
// deployments look like "acme-web"). Returns "" if there is no such token, which
// keeps the deployment-targeted playbooks from firing on vague, single-word, or
// namespace-prefix requests — those fall through to the adaptive loop.
func appEntity(toks []string) string {
	for _, t := range toks {
		if appLike(t) && strings.Contains(t, "-") {
			return t
		}
	}
	return ""
}

// focusKeywords are the leftover distinctive words (e.g. "github", "oauth") that
// name a subsystem the operator cares about, used to prioritize matching log lines.
func focusKeywords(toks []string, app string) []string {
	var f []string
	seen := map[string]bool{}
	for _, t := range toks {
		if t == app || len(t) < 4 || seen[t] {
			continue
		}
		if diagTriggers[t] || listVerbs[t] || selectorPreps[t] || selectorNoise[t] ||
			connectives[t] || resourceAlias[t] != "" {
			continue
		}
		seen[t] = true
		f = append(f, t)
	}
	return f
}

// tokenize lowercases and splits on any rune that can't appear in a kubectl name
// (so "acme-web" stays one token but punctuation is dropped).
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_')
	})
}

// Row is one matched resource: its namespace and name.
type Row struct {
	Namespace string
	Name      string
}

// FilterRows parses the table from `kubectl get <res> -A` and returns the rows whose
// NAME or NAMESPACE contains the selector (case-insensitive). Matching either column
// means "configmap list for acme-web" finds objects named acme-web-* AND any
// object living in an acme-web namespace — both reasonable readings of the ask.
func FilterRows(table, selector string) []Row {
	sel := strings.ToLower(selector)
	var rows []Row
	for _, raw := range strings.Split(table, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Skip the header row (`NAMESPACE NAME ...`). We always list with -A, so the
		// first column is the namespace and the second is the object name.
		if strings.EqualFold(fields[0], "NAMESPACE") || strings.EqualFold(fields[0], "NAME") {
			continue
		}
		ns, name := fields[0], fields[1]
		if strings.Contains(strings.ToLower(name), sel) || strings.Contains(strings.ToLower(ns), sel) {
			rows = append(rows, Row{Namespace: ns, Name: name})
		}
	}
	return rows
}

// Summarize renders the deterministic conclusion. With matches it lists every one
// (name + namespace); with none — having scanned ALL namespaces — it states a
// genuine, trustworthy absence (the thing a fixated model gets wrong).
func Summarize(display, selector string, rows []Row) string {
	if len(rows) == 0 {
		return fmt.Sprintf("No %s matching %q exist in any namespace (checked all namespaces with `kubectl get %s -A`).",
			display, selector, display)
	}
	// Group namespaces per object name so a configmap present in several namespaces
	// reads as one line, not three.
	byName := map[string][]string{}
	var order []string
	for _, r := range rows {
		if _, seen := byName[r.Name]; !seen {
			order = append(order, r.Name)
		}
		byName[r.Name] = append(byName[r.Name], r.Namespace)
	}
	sort.Strings(order)
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d %s matching %q across the cluster:\n", len(rows), display, selector)
	for _, name := range order {
		ns := byName[name]
		sort.Strings(ns)
		fmt.Fprintf(&b, "  - %s  (namespace%s %s)\n", name, plural(ns), strings.Join(ns, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(s []string) string {
	if len(s) == 1 {
		return ""
	}
	return "s"
}
