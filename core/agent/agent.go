// Package agent orchestrates Sahayak's core loop: turn a natural-language request
// into an inspectable Plan, gate every step behind human approval, run approved
// steps, and on failure run a diagnosis pass that can propose a follow-up — which
// re-enters the same gate. This is the bespoke "deep-agent" loop; sub-agents,
// virtual FS, and persistent memory layer on in later phases.
package agent

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/cartridge"
	"github.com/ZentienceLabs/sahayak-cli/core/diagnose"
	"github.com/ZentienceLabs/sahayak-cli/core/exec"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/redact"
	"github.com/ZentienceLabs/sahayak-cli/core/router"
	"github.com/ZentienceLabs/sahayak-cli/core/ui"
)

// Retriever grounds the model in installed knowledge packs. The knowledge package
// implements it; agent depends only on this narrow interface.
type Retriever interface {
	Search(ctx context.Context, query string, k int) ([]Grounding, error)
	Empty() bool
}

// Grounding is one retrieved documentation snippet used to ground a prompt.
type Grounding struct {
	Text   string
	Source string
}

// Learner records deterministically-judged observations for self-learning. The agent
// depends only on this narrow interface; core/learn implements it via an adapter.
type Learner interface {
	Record(kind, request, command string, args []string, cartridge, intent string, success bool)
}

// Memorizer gives the agent long-term recall across sessions. The memory package
// implements it; agent depends only on this interface.
type Memorizer interface {
	Recall(ctx context.Context, query string, k int) ([]string, error)
	Remember(ctx context.Context, text string) error
}

// EnvCache is the environment-topology cache (core/envfacts). The investigate loop
// uses it to (1) seed the first step with already-known, slowly-changing topology
// so it can skip re-discovery, (2) learn new durable facts from read-only output,
// and (3) self-invalidate a cached name the moment a command using it fails. The
// decision of what is durable enough to cache lives entirely in the implementation
// (deterministic, kind-based) — the agent just feeds it observations.
type EnvCache interface {
	// Hint renders known topology as a short grounding block ("" when empty).
	Hint() string
	// LearnFromKubectl mines durable facts from a successful read-only get.
	LearnFromKubectl(args []string, stdout string) int
	// InvalidateFromError drops a cached fact named in a kubectl NotFound stderr.
	InvalidateFromError(stderr string) int
}

// Agent wires a model Provider to the safe runner, redactor, and approval gate.
type Agent struct {
	Provider llm.Provider
	Runner   *exec.Runner
	Redactor *redact.Redactor
	Approver Approver
	Out      io.Writer
	UI       *ui.Printer

	// Retriever, when set and non-empty, injects grounding docs into the planner.
	Retriever Retriever
	// Memory, when set, recalls relevant past context and records each handled
	// request for future sessions.
	Memory Memorizer
	// Env, when set, supplies cached environment topology (namespaces, deployments)
	// to seed and accelerate investigation, and learns from each run.
	Env EnvCache
	// Router, when set, is the semantic/data-driven intent router (core/router). It is
	// consulted AFTER the regex playbooks and BEFORE the model classifier: it matches a
	// request to the nearest catalog example by meaning, widening phrasing coverage
	// without a code change per phrasing. nil disables it (regex + classifier only).
	Router *router.Router

	// Cartridges, when set, is the cross-cartridge index (core/cartridge): the
	// data-driven, tool-agnostic routing+template engine. Consulted at the TOP of
	// Investigate (most-precise, no model). It is the forward architecture; the regex
	// playbooks/router remain as the current k8s path until fully ported. nil disables it.
	Cartridges *cartridge.Index
	// cartridgeProbed guards the one-time applicability probing of cartridges.
	cartridgeProbed bool

	// Learner, when set, records deterministically-judged observations (routed run
	// outcomes, operator ad-hoc commands, unmatched requests) for the self-learning
	// suggestion engine. nil disables learning. It only OBSERVES — never changes behavior.
	Learner Learner

	// AutoRunReadOnly runs read-only steps without prompting when true.
	AutoRunReadOnly bool
	// MaxDiagnoseSteps bounds the diagnose→fix→diagnose loop.
	MaxDiagnoseSteps int
	// MaxInvestigateSteps bounds the iterative investigate loop.
	MaxInvestigateSteps int
}

// New builds an Agent with sensible Phase-1 defaults.
func New(p llm.Provider, approver Approver, out io.Writer) *Agent {
	return &Agent{
		Provider:            p,
		Runner:              exec.NewRunner(),
		Redactor:            redact.New(),
		Approver:            approver,
		Out:                 out,
		UI:                  ui.New(out),
		AutoRunReadOnly:     true,
		MaxDiagnoseSteps:    3,
		MaxInvestigateSteps: 8,
	}
}

// Handle runs a one-shot plan: the model proposes the whole plan up front, then
// Sahayak gates and runs each step. This is the explicit `--plan` path for direct,
// known actions; the default `ask` path is the adaptive Investigate loop, which
// reacts to each step's real output instead of committing to a fixed plan.
func (a *Agent) Handle(ctx context.Context, request string) error {
	a.UI.Banner("understanding your request")

	onTok, stop := a.UI.Streaming("thinking about: " + request)
	plan, err := a.plan(ctx, request, onTok)
	stop()
	if err != nil {
		return fmt.Errorf("planning failed: %w", err)
	}

	if plan.NeedMoreInfo != "" {
		a.UI.Info("\nI need more information:")
		a.UI.Note(plan.NeedMoreInfo)
		return nil
	}
	if len(plan.Steps) == 0 {
		a.UI.Info("\nNo commands proposed: " + plan.Summary)
		return nil
	}

	// If the plan references values it can't know yet (placeholders like <pod>),
	// a fixed up-front plan can't work — those values must be discovered from an
	// earlier step's output. Switch to the iterative investigate loop.
	for _, s := range plan.Steps {
		if s.HasPlaceholder() {
			a.UI.Note("plan needs a value it can't know yet (" + s.Pretty() + ") — switching to step-by-step investigation")
			return a.Investigate(ctx, request)
		}
	}

	a.UI.Info("\n" + plan.Summary)
	total := len(plan.Steps)
	for i, step := range plan.Steps {
		res, ran, err := a.runStepGated(ctx, step, i, total)
		if err != nil {
			return err
		}
		if !ran {
			continue // skipped
		}
		if !res.Success() {
			if err := a.diagnoseLoop(ctx, res); err != nil {
				return err
			}
		}
	}
	return nil
}

// RunOnce gates and runs a single operator-supplied command — the shell's `!` escape
// hatch for when Sahayak has no verified procedure and the operator wants to run the
// command themselves. Read-only commands auto-run; mutations still pass the approval
// gate, so even a hand-typed destructive command is confirmed before it executes.
func (a *Agent) RunOnce(ctx context.Context, command string, args []string) error {
	step := llm.Step{Command: command, Args: args, Explanation: "operator-supplied command (! escape hatch)"}
	res, ran, err := a.runStepGated(ctx, step, 0, 1)
	if ran && a.Learner != nil {
		// An operator command that succeeded is a deterministic "this works" signal —
		// a candidate to templatize into a playbook.
		a.Learner.Record("adhoc", command+" "+strings.Join(args, " "), command, args, "", "", res.Success())
	}
	if err == errAborted {
		return nil
	}
	return err
}

// runStepGated classifies a step, applies the approval policy, runs it if cleared,
// and prints the outcome. Returns (result, ran, error); ran is false when skipped
// or rejected. An operator edit that ESCALATES the risk tier is re-classified and
// re-confirmed, so a read-only step edited into a destructive one can't slip
// through on the original tier's approval.
func (a *Agent) runStepGated(ctx context.Context, step llm.Step, index, total int) (exec.Result, bool, error) {
	cur := step
	for {
		// Defense-in-depth: never execute an unresolved placeholder, even if the
		// auto-upgrade and investigate guards were somehow bypassed.
		if cur.HasPlaceholder() {
			fmt.Fprintf(a.Out, "\n  ⤫ skipping step with an unresolved placeholder: %s\n", cur.Pretty())
			return exec.Result{}, false, nil
		}
		risk := exec.Classify(cur.Command, cur.Args)

		// Read-only auto-run path (when enabled): no prompt, but still announced.
		if risk == exec.ReadOnly && a.AutoRunReadOnly {
			a.UI.StepHeader(index+1, total, cur.Pretty())
			a.UI.Reason(cur.Explanation)
			a.UI.Risk("read-only", "✓", int(exec.ReadOnly), true)
			return a.run(ctx, cur), true, nil
		}

		decision, finalStep, err := a.Approver.Review(cur, risk, index, total)
		if err != nil {
			return exec.Result{}, false, fmt.Errorf("approval failed: %w", err)
		}
		switch decision {
		case Reject:
			a.UI.Note("rejected — stopping.")
			return exec.Result{}, false, errAborted
		case Skip:
			a.UI.Note("skipped.")
			return exec.Result{}, false, nil
		case Approve:
			return a.runValidated(ctx, finalStep), true, nil
		case Edit:
			if newRisk := exec.Classify(finalStep.Command, finalStep.Args); newRisk > risk {
				a.UI.Note(fmt.Sprintf("edited command is now %s (was %s) — re-confirm.", newRisk, risk))
				cur = finalStep
				continue // re-gate at the higher tier
			}
			return a.runValidated(ctx, finalStep), true, nil
		default:
			return exec.Result{}, false, nil
		}
	}
}

// runValidated runs a step, but for a kubectl mutation that supports a server-side
// dry run it VALIDATES FIRST: the dry run must pass, or the real command is NOT
// executed and the operator sees the precise API error. The verdict comes from the
// cluster's own admission/validation — a deterministic critic — not the model. In the
// investigate loop a dry-run rejection is recorded as a failed observation, so the
// model naturally proposes a corrected step next turn (propose → Go disposes → repair).
func (a *Agent) runValidated(ctx context.Context, step llm.Step) exec.Result {
	if dargs, ok := exec.DryRunArgs(step.Command, step.Args); ok {
		stop := a.UI.Spin("validating with a server dry-run: " + step.Pretty())
		dr := a.Runner.Run(ctx, step.Command, dargs)
		stop()
		if !dr.Success() {
			a.UI.Failure(dr.ExitCode, dr.DurationMS)
			a.UI.Output(a.displayOutput(dr))
			a.UI.Note("server dry-run rejected this command — NOT executing it. Fix it (or run it yourself with `!`) and retry.")
			return dr
		}
		a.UI.Note("server dry-run passed — applying for real")
	}
	return a.run(ctx, step)
}

// run executes a step (under a spinner) and prints a styled outcome.
func (a *Agent) run(ctx context.Context, step llm.Step) exec.Result {
	stop := a.UI.Spin("running: " + step.Pretty())
	res := a.Runner.Run(ctx, step.Command, step.Args)
	stop()

	if res.Err != nil {
		a.UI.StartError(res.Err.Error())
		return res
	}
	if res.Success() {
		a.UI.Success(res.DurationMS)
		a.UI.Output(a.displayOutput(res))
	} else {
		a.UI.Failure(res.ExitCode, res.DurationMS)
		a.UI.Output(a.displayOutput(res))
	}
	return res
}

// displayOutput chooses what the operator sees for a command's output: for pod
// listings, the table plus the computed health line; for logs, the extracted error
// digest (not the raw noise); otherwise the trimmed raw output.
func (a *Agent) displayOutput(res exec.Result) string {
	if isKubectlGetPods(res) {
		raw := a.Redactor.String(trimOutput(res.Stdout))
		if s := podHealthSummary(a.Redactor.String(res.Stdout)); s != "" {
			return raw + "\n\n" + s
		}
		return raw
	}
	if isLogOutput(res) {
		return logErrorSummary(a.Redactor.String(res.Stdout) + "\n" + a.Redactor.String(res.Stderr))
	}
	if out := a.Redactor.String(trimOutput(res.Stdout)); out != "" {
		return out
	}
	return a.Redactor.String(trimOutput(res.Stderr))
}

// plan asks the model for a structured Plan grounded in machine context and, when
// available, retrieved knowledge-pack documentation.
func (a *Agent) plan(ctx context.Context, request string, onTok func(string)) (llm.Plan, error) {
	user := fmt.Sprintf("Machine context:\n%s\n%s%sRequest: %s",
		machineContext(), a.recall(ctx, request), a.grounding(ctx, request), request)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: planSystemPrompt},
		{Role: llm.RoleUser, Content: user},
	}
	resp, err := a.Provider.Chat(ctx, llm.ChatRequest{Messages: msgs, Temperature: 0.1, JSONOnly: true, JSONSchema: llm.PlanSchema, OnToken: onTok})
	if err != nil {
		return llm.Plan{}, err
	}
	return llm.ParsePlan(resp.Content)
}

// grounding retrieves relevant doc snippets and renders them as a reference block
// for the prompt. Returns "" when no retriever is configured or nothing matches.
func (a *Agent) grounding(ctx context.Context, query string) string {
	if a.Retriever == nil || a.Retriever.Empty() {
		return ""
	}
	hits, err := a.Retriever.Search(ctx, query, 5)
	if err != nil || len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Reference documentation (from installed knowledge packs — prefer documented flags; cite the source):\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "- [%s] %s\n", h.Source, oneLine(h.Text))
	}
	b.WriteString("\n")
	return b.String()
}

// recall pulls relevant memories from prior sessions into the planning context.
func (a *Agent) recall(ctx context.Context, query string) string {
	if a.Memory == nil {
		return ""
	}
	mems, err := a.Memory.Recall(ctx, query, 3)
	if err != nil || len(mems) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Relevant memory from past sessions (context only — still require approval):\n")
	for _, m := range mems {
		fmt.Fprintf(&b, "- %s\n", oneLine(m))
	}
	b.WriteString("\n")
	return b.String()
}

// diagnoseLoop runs the diagnosis engine on a failed result and, while the model
// proposes safe follow-ups and the operator approves them, keeps going up to
// MaxDiagnoseSteps.
func (a *Agent) diagnoseLoop(ctx context.Context, failed exec.Result) error {
	for i := 0; i < a.MaxDiagnoseSteps; i++ {
		// Deterministic pass first: recognized signals are printed and fed to the model.
		report := diagnose.Analyze(failed.Command, failed.ExitCode,
			a.Redactor.String(failed.Stdout), a.Redactor.String(effectiveStderr(failed)))
		for _, s := range report.Signals {
			a.UI.Note(s.Hint)
		}

		onTok, stop := a.UI.Streaming("diagnosing the failure")
		diag, err := a.diagnose(ctx, failed, report, onTok)
		stop()
		if err != nil {
			a.UI.Note(fmt.Sprintf("diagnosis unavailable: %v", err))
			return nil
		}
		a.UI.Finding(fmt.Sprintf("root cause (%s): %s", diag.Confidence, diag.RootCause))
		if diag.NextStep == nil {
			return nil
		}
		res, ran, err := a.runStepGated(ctx, *diag.NextStep, 0, 1)
		if err != nil {
			if err == errAborted {
				return nil // operator chose to stop; not a program error
			}
			return err
		}
		if !ran || res.Success() {
			return nil
		}
		failed = res // the follow-up also failed; diagnose again
	}
	a.UI.Note("reached diagnosis step limit")
	return nil
}

// diagnose sends the redacted failure context plus deterministic signals to the
// model for root-cause analysis.
func (a *Agent) diagnose(ctx context.Context, res exec.Result, report diagnose.Report, onTok func(string)) (llm.Diagnosis, error) {
	ctxText := fmt.Sprintf(
		"Command: %s %v\nExit code: %d\nstdout:\n%s\nstderr:\n%s\n%s",
		res.Command, res.Args, res.ExitCode,
		a.Redactor.String(trimOutput(res.Stdout)),
		a.Redactor.String(trimOutput(effectiveStderr(res))),
		report.PromptHints(),
	)
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: diagnoseSystemPrompt},
		{Role: llm.RoleUser, Content: ctxText},
	}
	resp, err := a.Provider.Chat(ctx, llm.ChatRequest{Messages: msgs, Temperature: 0.1, JSONOnly: true, JSONSchema: llm.DiagnosisSchema, OnToken: onTok})
	if err != nil {
		return llm.Diagnosis{}, err
	}
	return llm.ParseDiagnosis(resp.Content)
}
