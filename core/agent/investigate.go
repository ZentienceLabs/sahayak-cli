package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/exec"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
)

// observation records one executed step and the condensed result fed back to the
// model on the next turn.
type observation struct {
	command string
	exitOK  bool
	text    string
}

// Investigate runs the iterative ("deep agent") loop: the model proposes ONE next
// command at a time, Sahayak runs it (gated), condenses + keyword-filters the
// output in Go (the safe stand-in for grep), and feeds it back — until the model
// concludes or the step budget is hit. This is what lets it discover real names
// (list namespaces → match "acme" → drill into the live pods) instead of
// guessing a whole plan up front.
func (a *Agent) Investigate(ctx context.Context, request string) error {
	// Deterministic fast path first: if this is a recognized, unambiguous intent
	// (e.g. "list configmaps for acme-web"), handle it without the model's
	// plan/stop judgment — one read-only listing, filtered and concluded in Go.
	// Data-driven cartridge engine (the architecture): route through the cross-cartridge
	// index (peer cartridges, by meaning) and run the grounded template. When it is set
	// it is the SOLE deterministic router — the legacy regex playbooks / semantic router /
	// model classifier / composite matcher are bypassed, so a miss falls straight to the
	// investigate loop. (The legacy pipeline still runs when Cartridges is nil, e.g.
	// SAHAYAK_LEGACY=1, for comparison during the migration.)
	if a.Cartridges != nil {
		if handled, err := a.tryCartridge(ctx, request); err != nil {
			return err
		} else if handled {
			return nil
		}
		// Nothing matched a tool — a deterministic "uncovered request" signal for learning.
		if a.Learner != nil {
			a.Learner.Record("missed", request, "", nil, "", "", false)
		}
	} else {
		if handled, err := a.legacyRoute(ctx, request); err != nil {
			return err
		} else if handled {
			return nil
		}
	}

	max := a.MaxInvestigateSteps
	if max <= 0 {
		max = 8
	}
	kws := extractKeywords(request)
	a.UI.Banner("investigating: " + request)
	if len(kws) > 0 {
		a.UI.Note("matching on: " + strings.Join(kws, ", "))
	}

	var obs []observation
	ran := map[string]bool{}
	lastNS := ""    // namespace discovered from a prior `-n X`, reused if the model drops it
	noProgress := 0 // consecutive turns that ran no new command

	for step := 0; step < max; step++ {
		onTok, stop := a.UI.Streaming("thinking about the next step")
		na, err := a.nextAction(ctx, request, obs, step, onTok)
		stop()
		if err != nil {
			return fmt.Errorf("investigation step failed: %w", err)
		}
		if na.Thought != "" {
			a.UI.Thought(oneLine(na.Thought))
		}
		if na.Done || na.Action == nil {
			a.concludeInvestigation(ctx, request, na.FinalAnswer, obs)
			return nil
		}

		key := na.Action.Command + " " + strings.Join(na.Action.Args, " ")

		// Never run a placeholder; instead tell the model to discover the real value
		// and loop — this is how it self-corrects rather than failing on "<pod>".
		if na.Action.HasPlaceholder() {
			a.UI.Note("that command still has a placeholder (" + na.Action.Pretty() + ") — discovering the real value first")
			obs = append(obs, observation{command: key,
				text: "(NOT RUN: this command contained a placeholder like <pod>. You must FIRST list/discover the real name from a previous observation, then use that exact literal name — never <...>.)"})
			if noProgress++; noProgress >= 2 {
				break
			}
			continue
		}
		// Deterministic repair: if the model dropped the namespace on a namespaced
		// kubectl command, re-attach the one we already discovered. This fixes the
		// common "fell back to namespace default → NotFound" failure.
		if repaired, ns, injected := repairNamespace(*na.Action, lastNS); injected {
			a.UI.Note("re-attaching discovered namespace: -n " + ns)
			na.Action = &repaired
		}
		if ns, ok := namespaceOf(na.Action.Args); ok {
			lastNS = ns
		}
		// Strip a contradictory namespace: `-n X --all-namespaces` is self-conflicting
		// (kubectl silently ignores -n and lists everything). Drop the -n so the command
		// reads honestly as cluster-wide.
		if cleaned, ok := dropRedundantNamespace(*na.Action); ok {
			na.Action = &cleaned
		}
		// Cap log fetches so a single command can't pull 150k lines.
		if limited, ok := maybeLimitLogTail(*na.Action); ok {
			na.Action = &limited
		}

		key = na.Action.Command + " " + strings.Join(na.Action.Args, " ")
		if ran[key] {
			obs = append(obs, observation{command: key, exitOK: true,
				text: "(skipped — this exact command already ran; choose a different step or finish)"})
			if noProgress++; noProgress >= 2 {
				break
			}
			continue
		}
		ran[key] = true
		noProgress = 0

		res, didRun, err := a.runStepGated(ctx, *na.Action, step, max)
		if err != nil {
			if err == errAborted {
				a.UI.Note("investigation stopped by operator.")
				return nil
			}
			return err
		}
		if !didRun {
			obs = append(obs, observation{command: key, text: "(skipped by operator)"})
			continue
		}
		obs = append(obs, observation{
			command: key,
			exitOK:  res.Success(),
			text:    a.condense(res, kws),
		})

		// Feed the environment-fact cache: learn durable topology from successful
		// read-only output, or self-invalidate a stale cached name when the command
		// failed because the object is gone. The cache decides what's worth keeping.
		if a.Env != nil && strings.EqualFold(na.Action.Command, "kubectl") {
			if res.Success() {
				if learned := a.Env.LearnFromKubectl(na.Action.Args, a.Redactor.String(res.Stdout)); learned > 0 {
					a.UI.Note(fmt.Sprintf("remembered %d stable fact(s) about your environment", learned))
				}
			} else if dropped := a.Env.InvalidateFromError(a.Redactor.String(effectiveStderr(res))); dropped > 0 {
				a.UI.Note(fmt.Sprintf("a cached name was stale — forgot %d fact(s)", dropped))
			}
		}
	}

	if noProgress >= 2 {
		a.UI.Note("no further progress — concluding from what was found")
	} else {
		a.UI.Note(fmt.Sprintf("reached the %d-step investigation budget", max))
	}
	a.concludeInvestigation(ctx, request, a.forceConclude(ctx, request, obs), obs)
	return nil
}

// nextAction asks the model for the single next step given observations so far.
// To keep CPU inference fast, the heavy, static context (machine info, knowledge
// grounding, memory) is sent ONLY on the first step; later steps carry just the
// goal and a capped observation log, so each call's prompt stays small.
func (a *Agent) nextAction(ctx context.Context, request string, obs []observation, step int, onTok func(string)) (llm.NextAction, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Goal: %s\n\n", request)
	if step == 0 {
		fmt.Fprintf(&b, "Machine context:\n%s\n", machineContext())
		if a.Env != nil {
			if hint := a.Env.Hint(); hint != "" {
				b.WriteString(hint)
			}
		}
		if g := a.grounding(ctx, request); g != "" {
			b.WriteString(g)
		}
		if r := a.recall(ctx, request); r != "" {
			b.WriteString(r)
		}
	}
	b.WriteString(renderObservations(obs))

	resp, err := a.Provider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: investigateSystemPrompt},
			{Role: llm.RoleUser, Content: b.String()},
		},
		Temperature: 0.1,
		JSONOnly:    true,
		JSONSchema:  llm.NextActionSchema,
		OnToken:     onTok,
	})
	if err != nil {
		return llm.NextAction{}, err
	}
	return llm.ParseNextAction(resp.Content)
}

// forceConclude asks the model for a best-effort final answer when the step budget
// is exhausted. Returns "" if it can't produce one.
func (a *Agent) forceConclude(ctx context.Context, request string, obs []observation) string {
	onTok, stop := a.UI.Streaming("summarizing findings")
	resp, err := a.Provider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: investigateSystemPrompt + "\nYou are OUT OF STEPS. Respond with done=true and your best final_answer from what you observed."},
			{Role: llm.RoleUser, Content: "Goal: " + request + "\n\n" + renderObservations(obs)},
		},
		Temperature: 0.1,
		JSONOnly:    true,
		JSONSchema:  llm.NextActionSchema,
		OnToken:     onTok,
	})
	stop()
	if err == nil {
		if na, perr := llm.ParseNextAction(resp.Content); perr == nil && strings.TrimSpace(na.FinalAnswer) != "" {
			return na.FinalAnswer
		}
	}
	// The model gave nothing usable — synthesize a conclusion from what we observed.
	return deterministicConclusion(obs)
}

// deterministicConclusion builds a best-effort answer from observations when the
// model can't — preferring the most recent computed HEALTH SUMMARY, so the run
// never ends with a useless "no conclusion" if we actually learned something.
func deterministicConclusion(obs []observation) string {
	// Prefer a log analysis (most specific), then a pod-health summary.
	for i := len(obs) - 1; i >= 0; i-- {
		if strings.Contains(obs[i].text, "LOG ANALYSIS") {
			return "From the logs — " + strings.TrimSpace(obs[i].text)
		}
	}
	for i := len(obs) - 1; i >= 0; i-- {
		if idx := strings.Index(obs[i].text, "HEALTH SUMMARY"); idx >= 0 {
			line := obs[i].text[idx:]
			if nl := strings.IndexByte(line, '\n'); nl >= 0 {
				line = line[:nl]
			}
			return "From the pod inspection — " + strings.TrimSpace(strings.TrimPrefix(line, "HEALTH SUMMARY (computed by Sahayak):"))
		}
	}
	var okCmds []string
	for _, o := range obs {
		if o.exitOK {
			okCmds = append(okCmds, o.command)
		}
	}
	if len(okCmds) > 0 {
		return "Inspected: " + strings.Join(okCmds, "; ") + ". Nothing clearly failing was surfaced; re-run with a stronger model for a deeper read."
	}
	return ""
}

// concludeInvestigation prints the final answer. Before doing so it runs the
// verify-before-concluding-absence guard: a weak model can fixate on one namespace
// and report "X doesn't exist" even though it already listed X elsewhere. If our
// own observations contradict an absence claim, we surface the real matches instead
// of the model's wrong conclusion.
func (a *Agent) concludeInvestigation(ctx context.Context, request, answer string, obs []observation) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = honestNoConclusion(obs)
	}
	if corrected, ok := crossCheckAbsence(request, answer, obs); ok {
		a.UI.Note("the draft conclusion said “not found”, but earlier output contains matches — correcting from observed data")
		answer = corrected
	}
	a.UI.Conclusion(answer)
	// NOTE: we deliberately do NOT auto-remember investigation conclusions. They can
	// be wrong, and live cluster state goes stale — recalling them later poisons
	// future runs (e.g. a typo'd namespace name). Only user-curated `memory add`
	// facts are persisted. Live questions should always re-discover from scratch.
}

// honestNoConclusion is the fallback message when neither a playbook, the semantic
// router, the classifier, nor the investigate loop produced a usable answer. Rather
// than a vague "no conclusion", it says so plainly and points the operator at the two
// things that actually work: a phrasing the deterministic playbooks cover, or running
// the command themselves. This is the "honest I-don't-know" the architecture calls for
// — never a fabricated answer or a guessed command.
func honestNoConclusion(obs []observation) string {
	ranAny := false
	for _, o := range obs {
		if o.exitOK {
			ranAny = true
			break
		}
	}
	var b strings.Builder
	b.WriteString("I couldn't reach a verified conclusion for this request — and I won't guess.\n")
	if !ranAny {
		b.WriteString("  • No deterministic playbook matched it, and the step-by-step pass found no safe command to run.\n")
	} else {
		b.WriteString("  • The commands I ran didn't surface a clear answer.\n")
	}
	b.WriteString("  • Try phrasing it around a resource and an app, which the playbooks handle reliably:\n")
	b.WriteString("      \"logs for acme-web\" · \"list configmaps for acme-web\" · \"is FLAG set in acme-web\" · \"what image is acme-web running\"\n")
	b.WriteString("  • Or run the exact command yourself (in the shell, prefix it with `!`) and ask me to read the output.")
	return b.String()
}

// absenceRe detects a conclusion that asserts something was NOT found / does not
// exist. These are exactly the conclusions worth double-checking against the data
// we actually gathered, because a fixated model reports them wrongly.
var absenceRe = regexp.MustCompile(`(?i)\b(no|not|none|never|doesn't|does not|don't|do not|isn't|aren't|cannot|can't|couldn't|could not|unable|no such)\b`)

// resourceTypeWords are kubectl resource nouns — poor "name" keywords (they match
// the query, not a specific object), so they're excluded from name matching.
var resourceTypeWords = map[string]bool{
	"configmap": true, "configmaps": true, "secret": true, "secrets": true,
	"ingress": true, "ingresses": true, "node": true, "nodes": true,
	"job": true, "jobs": true, "statefulset": true, "statefulsets": true,
	"replicaset": true, "replicasets": true, "endpoint": true, "endpoints": true,
}

// nameKeywords narrows the request keywords to ones that look like a specific
// object NAME the user is after (e.g. "acme-web"), dropping resource-type nouns
// and short noise so the absence cross-check matches real rows, not query words.
func nameKeywords(request string) []string {
	var out []string
	for _, k := range extractKeywords(request) {
		if len(k) < 4 || resourceTypeWords[k] {
			continue
		}
		out = append(out, k)
	}
	return out
}

// crossCheckAbsence is the deterministic guard. If answer claims absence AND the
// successful observations contain resource rows whose names match a name the user
// asked about, it returns a corrected answer listing those matches. Returns
// (corrected, true) when it overrides, ("", false) otherwise. No model call.
// healthAbsenceRe matches conclusions whose "absence" is about ERRORS/problems, not
// about the existence of a named object ("no errors", "all healthy", "everything is
// Running"). Finding rows that match a keyword does NOT contradict these, so the
// cross-check must NOT override them — otherwise a correct "no errors" answer gets
// reframed as "found 6 matches".
var healthAbsenceRe = regexp.MustCompile(`(?i)no (errors?|issues?|problems?|failures?|crashes?)|NO ERRORS|all (running|ready|healthy)|everything (is )?(running|ready|healthy|fine)|look(s)? (clean|healthy)`)

func crossCheckAbsence(request, answer string, obs []observation) (string, bool) {
	if !absenceRe.MatchString(answer) {
		return "", false
	}
	// Don't "correct" a health conclusion ("no errors", "all healthy"): matching
	// resource rows is not evidence against it.
	if healthAbsenceRe.MatchString(answer) {
		return "", false
	}
	kws := nameKeywords(request)
	if len(kws) == 0 {
		return "", false
	}
	matchedKW := map[string]bool{}
	seen := map[string]bool{}
	var rows []string
	for _, o := range obs {
		if !o.exitOK {
			continue
		}
		for _, line := range strings.Split(o.text, "\n") {
			l := strings.TrimSpace(line)
			if !looksLikeResourceRow(l) {
				continue
			}
			ll := strings.ToLower(l)
			for _, k := range kws {
				if strings.Contains(ll, k) {
					matchedKW[k] = true
					if !seen[l] {
						seen[l] = true
						rows = append(rows, l)
					}
				}
			}
		}
	}
	if len(rows) == 0 {
		return "", false
	}
	var names []string
	for _, k := range kws {
		if matchedKW[k] {
			names = append(names, k)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d match(es) for '%s' in the output already gathered (the earlier \"not found\" was scoped to a single namespace, but these exist elsewhere):\n",
		len(rows), strings.Join(names, "', '"))
	for _, r := range rows {
		b.WriteString("  " + r + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// looksLikeResourceRow reports whether a line is a data row from a kubectl listing
// (not a header, annotation, or our own "(omitted …)" note) — so the cross-check
// only matches real objects.
func looksLikeResourceRow(l string) bool {
	// Accept both table rows ("NAME  DATA  AGE" columns) and single-column `-o name`
	// output ("configmap/acme-web-config"); the specific keyword match is what
	// makes a line relevant, so we only reject headers/annotations here.
	if strings.TrimSpace(l) == "" {
		return false
	}
	if strings.HasPrefix(l, "(") || strings.HasPrefix(l, "…") || strings.HasPrefix(l, "...") {
		return false
	}
	upper := strings.ToUpper(l)
	if strings.HasPrefix(upper, "NAME ") || strings.HasPrefix(upper, "NAMESPACE ") {
		return false
	}
	if strings.Contains(l, "omitted") || strings.Contains(l, "showing") {
		return false
	}
	return true
}

// dropRedundantNamespace removes `-n X` from a kubectl command that also passes
// `-A`/`--all-namespaces` (the two contradict; kubectl ignores -n and goes
// cluster-wide). Returns (cleaned, true) when it stripped one.
func dropRedundantNamespace(s llm.Step) (llm.Step, bool) {
	if strings.ToLower(s.Command) != "kubectl" {
		return s, false
	}
	if !hasFlag(s.Args, "-A") && !hasFlag(s.Args, "--all-namespaces") {
		return s, false
	}
	if _, has := namespaceOf(s.Args); !has {
		return s, false
	}
	out := make([]string, 0, len(s.Args))
	for i := 0; i < len(s.Args); i++ {
		a := s.Args[i]
		if a == "-n" || a == "--namespace" {
			i++ // skip the flag and its value
			continue
		}
		if strings.HasPrefix(a, "--namespace=") || strings.HasPrefix(a, "-n=") {
			continue
		}
		out = append(out, a)
	}
	s.Args = out
	return s, true
}

// recentObservations caps how many full observations are sent to the model. Older
// ones collapse to a one-line command+status, keeping CPU prefill cheap while the
// model still sees recent detail (which is what it reasons over).
const recentObservations = 3

// renderObservations formats the running log for the next prompt, with older
// observations summarized to one line to bound the prompt size.
func renderObservations(obs []observation) string {
	if len(obs) == 0 {
		return "No observations yet. Propose the first discovery step (usually a broad read-only listing)."
	}
	var b strings.Builder
	b.WriteString("Observations so far:\n")
	cutoff := len(obs) - recentObservations
	for i, o := range obs {
		status := "ok"
		if !o.exitOK {
			status = "FAILED"
		}
		if i < cutoff {
			// Older: one-line summary only (the model already acted on these).
			fmt.Fprintf(&b, "[%d] $ %s  (%s)\n", i+1, o.command, status)
			continue
		}
		fmt.Fprintf(&b, "[%d] $ %s  (%s)\n%s\n", i+1, o.command, status, indentBlock(o.text))
	}
	return b.String()
}

// condense turns command output into a compact, relevant observation. With no
// shell pipes available, this IS Sahayak's safe "grep": for large output it keeps
// the header line plus rows matching the request keywords or showing an abnormal
// state, so the model sees the signal without the noise. For `kubectl get pods` it
// also prepends a deterministic HEALTH SUMMARY so a weak model can't misread it.
func (a *Agent) condense(res exec.Result, kws []string) string {
	if isKubectlGetPods(res) {
		if summary := podHealthSummary(a.Redactor.String(res.Stdout)); summary != "" {
			return summary + "\n" + a.condenseRaw(res, kws)
		}
	}
	if isLogOutput(res) {
		// Logs get error-extracted, not raw-dumped — stdout and stderr both scanned.
		return logErrorSummary(a.Redactor.String(res.Stdout) + "\n" + a.Redactor.String(res.Stderr))
	}
	return a.condenseRaw(res, kws)
}

// condenseRaw is the keyword/abnormal-line filtering described above.
func (a *Agent) condenseRaw(res exec.Result, kws []string) string {
	body := strings.TrimRight(a.Redactor.String(res.Stdout), "\n")
	if e := strings.TrimRight(a.Redactor.String(effectiveStderr(res)), "\n"); e != "" {
		if body != "" {
			body += "\n"
		}
		body += e
	}
	body = strings.TrimSpace(body)
	if body == "" {
		if res.Success() {
			return "(no output; exit 0)"
		}
		return fmt.Sprintf("(no output; exit %d)", res.ExitCode)
	}

	lines := strings.Split(body, "\n")
	const fullThreshold = 25
	if len(lines) <= fullThreshold {
		return body
	}

	kept := []string{lines[0]} // header row (e.g. NAME STATUS …)
	matched := 0
	for _, l := range lines[1:] {
		if matchesKeyword(l, kws) || looksAbnormal(l) {
			kept = append(kept, l)
			matched++
			if len(kept) >= fullThreshold {
				break
			}
		}
	}
	if matched == 0 {
		// nothing matched: show the first lines so the model still has something.
		kept = lines[:fullThreshold]
		return strings.Join(kept, "\n") + fmt.Sprintf("\n(no keyword/abnormal matches; showing first %d of %d lines)", fullThreshold, len(lines))
	}
	return strings.Join(kept, "\n") + fmt.Sprintf("\n(showing %d relevant of %d lines)", matched, len(lines)-1)
}

// abnormalRe flags rows that suggest a problem worth surfacing regardless of keyword.
var abnormalRe = regexp.MustCompile(`(?i)(error|crashloop|imagepull|errimage|oomkill|backoff|fail|failed|pending|evicted|terminating|unhealthy|notready|not ready|\b0/[1-9]\b|\b[0-9]+/[0-9]+\b.*\b(0|[1-9][0-9]+)\b restart|denied|refused|timeout|exceeded)`)

func looksAbnormal(line string) bool { return abnormalRe.MatchString(line) }

func matchesKeyword(line string, kws []string) bool {
	if len(kws) == 0 {
		return false
	}
	ll := strings.ToLower(line)
	for _, k := range kws {
		if strings.Contains(ll, k) {
			return true
		}
	}
	return false
}

// stopwords are generic request words that make poor name-matching keywords. They
// must be filtered out because a generic word like "configmap" would match EVERY
// line of `kubectl get configmap -o name` output, drowning the one distinctive
// term (e.g. "acme-web") the operator actually cares about.
var stopwords = map[string]bool{
	"the": true, "any": true, "are": true, "is": true, "in": true, "there": true,
	"error": true, "errors": true, "issue": true, "issues": true, "problem": true,
	"application": true, "applications": true, "app": true, "apps": true,
	"show": true, "list": true, "find": true, "check": true, "get": true, "see": true,
	"me": true, "my": true, "of": true, "for": true, "and": true, "or": true, "on": true,
	"with": true, "status": true, "logs": true, "log": true, "pod": true, "pods": true,
	"namespace": true, "namespaces": true, "cluster": true, "kubernetes": true, "k8s": true,
	"service": true, "services": true, "deployment": true, "deployments": true,
	"running": true, "failing": true, "failed": true, "all": true, "what": true, "which": true,
	"how": true, "why": true, "do": true, "does": true, "to": true, "a": true, "an": true,
	// polite/filler words and verbs that carry no naming signal
	"can": true, "you": true, "your": true, "please": true, "provide": true, "give": true,
	"want": true, "need": true, "could": true, "would": true, "tell": true, "us": true,
	"our": true, "display": true, "from": true, "about": true, "into": true, "let": true,
	// resource-type nouns: they identify the query, not a specific object, so they
	// match every row and must not drive output filtering
	"configmap": true, "configmaps": true, "cm": true, "secret": true, "secrets": true,
	"ingress": true, "ingresses": true, "ing": true, "node": true, "nodes": true,
	"job": true, "jobs": true, "statefulset": true, "statefulsets": true, "sts": true,
	"replicaset": true, "replicasets": true, "endpoint": true, "endpoints": true,
	"daemonset": true, "daemonsets": true, "cronjob": true, "cronjobs": true,
	"svc": true, "deploy": true, "ns": true, "po": true, "rs": true, "ep": true,
}

// extractKeywords pulls distinctive terms from the request to drive output
// filtering (e.g. "is there errors in acme dev apps" → ["acme", "dev"]).
func extractKeywords(request string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.FieldsFunc(strings.ToLower(request), func(r rune) bool {
		return !isWordRune(r)
	}) {
		if len(tok) < 3 || stopwords[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

// namespaceOf returns the namespace named by -n/--namespace in args, if present.
func namespaceOf(args []string) (string, bool) {
	for i, a := range args {
		switch {
		case a == "-n" || a == "--namespace":
			if i+1 < len(args) {
				return args[i+1], true
			}
		case strings.HasPrefix(a, "--namespace="):
			return strings.TrimPrefix(a, "--namespace="), true
		case strings.HasPrefix(a, "-n="):
			return strings.TrimPrefix(a, "-n="), true
		}
	}
	return "", false
}

// clusterScoped kubectl resources do not take a namespace.
var clusterScoped = map[string]bool{
	"nodes": true, "node": true, "namespaces": true, "namespace": true, "ns": true,
	"pv": true, "persistentvolumes": true, "storageclass": true, "storageclasses": true,
	"clusterrole": true, "clusterroles": true, "clusterrolebinding": true, "clusterrolebindings": true,
	"componentstatuses": true, "cs": true, "pvc": false,
}

// namespacedVerbs are kubectl subcommands that operate within a namespace.
var namespacedVerbs = map[string]bool{
	"logs": true, "describe": true, "exec": true, "port-forward": true, "get": true,
	"delete": true, "edit": true, "rollout": true, "scale": true, "patch": true,
	"annotate": true, "label": true,
}

// repairNamespace re-attaches a known namespace to a namespaced kubectl command
// that is missing one — the deterministic fix for a model that drops "-n <ns>".
// Returns (possibly-modified step, the namespace used, whether it injected).
func repairNamespace(s llm.Step, lastNS string) (llm.Step, string, bool) {
	if lastNS == "" || strings.ToLower(s.Command) != "kubectl" {
		return s, "", false
	}
	if _, has := namespaceOf(s.Args); has {
		return s, "", false
	}
	if hasFlag(s.Args, "-A") || hasFlag(s.Args, "--all-namespaces") {
		return s, "", false
	}
	nonflags := nonFlagArgs(s.Args)
	if len(nonflags) == 0 || !namespacedVerbs[strings.ToLower(nonflags[0])] {
		return s, "", false
	}
	if strings.ToLower(nonflags[0]) == "get" && len(nonflags) >= 2 && clusterScoped[strings.ToLower(nonflags[1])] {
		return s, "", false
	}
	repaired := s
	repaired.Args = append(append([]string{}, s.Args...), "-n", lastNS)
	return repaired, lastNS, true
}

// maybeLimitLogTail appends "--tail=500" to a `kubectl logs` command that lacks a
// tail/limit, so Sahayak never fetches a 150k-line log (the 32-second mega-dump).
func maybeLimitLogTail(s llm.Step) (llm.Step, bool) {
	if strings.ToLower(s.Command) != "kubectl" {
		return s, false
	}
	nf := nonFlagArgs(s.Args)
	if len(nf) == 0 || strings.ToLower(nf[0]) != "logs" {
		return s, false
	}
	for _, a := range s.Args {
		if a == "--tail" || strings.HasPrefix(a, "--tail=") {
			return s, false
		}
	}
	rep := s
	rep.Args = append(append([]string{}, s.Args...), "--tail=500")
	return rep, true
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func nonFlagArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

// indentBlock indents a multi-line observation for readability in the prompt log.
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}
