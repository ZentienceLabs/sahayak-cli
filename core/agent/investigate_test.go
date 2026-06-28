package agent

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/exec"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/redact"
)

func llmStep(cmd string, args ...string) llm.Step { return llm.Step{Command: cmd, Args: args} }

// scriptProvider returns scripted model replies in order — lets us drive the whole
// loop deterministically with no real model.
type scriptProvider struct {
	replies []string
	i       int
}

func (s *scriptProvider) Name() string                   { return "script" }
func (s *scriptProvider) Health(_ context.Context) error { return nil }
func (s *scriptProvider) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	r := s.replies[len(s.replies)-1]
	if s.i < len(s.replies) {
		r = s.replies[s.i]
	}
	s.i++
	return llm.ChatResponse{Content: r}, nil
}

// yesApprover approves every step (for tests).
type yesApprover struct{}

func (yesApprover) Review(step llm.Step, _ exec.Risk, _, _ int) (Decision, llm.Step, error) {
	return Approve, step, nil
}

func TestInvestigateLoopEndToEnd(t *testing.T) {
	// Step 1: run a real, present command (`go version`). Step 2: conclude.
	replies := []string{
		`{"thought":"check the go toolchain","action":{"command":"go","args":["version"]},"done":false}`,
		`{"thought":"have what I need","done":true,"final_answer":"the go toolchain is installed and reachable"}`,
	}
	var out bytes.Buffer
	a := New(&scriptProvider{replies: replies}, yesApprover{}, &out)

	if err := a.Investigate(context.Background(), "is the go toolchain available"); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "go version go") { // the real output of `go version` proves it ran
		t.Fatalf("step did not run:\n%s", s)
	}
	if !strings.Contains(s, "Conclusion") || !strings.Contains(s, "toolchain is installed") {
		t.Fatalf("loop did not conclude:\n%s", s)
	}
}

func TestHonestNoConclusionMessage(t *testing.T) {
	// No observations ran → the message must be honest and actionable, never a fabricated
	// answer, and must point at both a playbook phrasing and the ! escape hatch.
	msg := honestNoConclusion(nil)
	for _, want := range []string{"won't guess", "acme-web", "`!`"} {
		if !strings.Contains(msg, want) {
			t.Errorf("honest fallback missing %q:\n%s", want, msg)
		}
	}
	// With a successful observation, the wording acknowledges commands ran.
	got := honestNoConclusion([]observation{{command: "kubectl get ns", exitOK: true, text: "x"}})
	if !strings.Contains(got, "didn't surface a clear answer") {
		t.Errorf("ran-commands wording missing:\n%s", got)
	}
}

func TestHandleAutoUpgradesOnPlaceholder(t *testing.T) {
	// One-shot plan contains a <pod> placeholder → must auto-switch to investigate.
	replies := []string{
		`{"summary":"check app logs","steps":[{"command":"kubectl","args":["logs","--previous","<pod>"],"explanation":"logs"}]}`,
		`{"thought":"pods are all healthy","done":true,"final_answer":"no errors: all pods Running"}`,
	}
	var out bytes.Buffer
	a := New(&scriptProvider{replies: replies}, yesApprover{}, &out)
	if err := a.Handle(context.Background(), "are there errors in the app"); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "step-by-step investigation") {
		t.Fatalf("expected auto-upgrade to investigate:\n%s", s)
	}
	if !strings.Contains(s, "Conclusion") || !strings.Contains(s, "no errors") {
		t.Fatalf("investigate did not conclude:\n%s", s)
	}
	// The placeholder command must never have executed.
	if strings.Contains(s, "not found") {
		t.Fatalf("placeholder command was executed:\n%s", s)
	}
}

func TestInvestigateRespectsStepBudget(t *testing.T) {
	// Always returns a fresh action, never done → must stop at the budget.
	never := []string{`{"thought":"keep looking","action":{"command":"go","args":["env","GOOS"]},"done":false}`}
	var out bytes.Buffer
	a := New(&scriptProvider{replies: never}, yesApprover{}, &out)
	a.MaxInvestigateSteps = 2
	if err := a.Investigate(context.Background(), "loop forever"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "investigation budget") {
		t.Fatalf("expected budget stop:\n%s", out.String())
	}
}

func TestExtractKeywords(t *testing.T) {
	got := extractKeywords("is there any errors in the acme dev applications")
	// generic words dropped; distinctive terms kept
	if !contains(got, "acme") || !contains(got, "dev") {
		t.Fatalf("expected acme+dev, got %v", got)
	}
	for _, bad := range []string{"errors", "applications", "the", "any", "is"} {
		if contains(got, bad) {
			t.Fatalf("stopword %q leaked: %v", bad, got)
		}
	}
}

func TestLooksAbnormal(t *testing.T) {
	abnormal := []string{
		"web-7c  0/1  CrashLoopBackOff  5  3m",
		"api-2   ImagePullBackOff",
		"db-0    Error",
		"job-x   OOMKilled",
	}
	for _, l := range abnormal {
		if !looksAbnormal(l) {
			t.Errorf("should be abnormal: %q", l)
		}
	}
	if looksAbnormal("web-1  1/1  Running  0  5d") {
		t.Errorf("healthy row flagged abnormal")
	}
}

func TestCondenseFiltersLargeOutput(t *testing.T) {
	a := &Agent{Redactor: redact.New()}
	var b strings.Builder
	b.WriteString("NAME           STATUS\n")
	for i := 0; i < 40; i++ {
		b.WriteString("other-ns-x     Active\n")
	}
	b.WriteString("acme-dev    Active\n")      // keyword match
	b.WriteString("broken-ns      Terminating\n") // abnormal
	res := exec.Result{Command: "kubectl", ExitCode: 0, Stdout: b.String()}

	out := a.condense(res, []string{"acme"})
	if !strings.Contains(out, "NAME") {
		t.Errorf("header dropped:\n%s", out)
	}
	if !strings.Contains(out, "acme-dev") {
		t.Errorf("keyword match dropped:\n%s", out)
	}
	if !strings.Contains(out, "broken-ns") {
		t.Errorf("abnormal row dropped:\n%s", out)
	}
	if !strings.Contains(out, "relevant of") {
		t.Errorf("expected a filtering note:\n%s", out)
	}
	// The noise should be gone (40 identical lines shouldn't all be present).
	if strings.Count(out, "other-ns-x") > 5 {
		t.Errorf("noise not filtered:\n%s", out)
	}
}

func TestCondenseSmallOutputKeptWhole(t *testing.T) {
	a := &Agent{Redactor: redact.New()}
	res := exec.Result{Command: "kubectl", ExitCode: 0, Stdout: "NAME\nacme-dev\nkube-system\n"}
	out := a.condense(res, []string{"acme"})
	if !strings.Contains(out, "kube-system") {
		t.Errorf("small output should be kept whole:\n%s", out)
	}
}

func TestRepairNamespace(t *testing.T) {
	// drops -n on logs → should re-attach
	s := llm.Step{Command: "kubectl", Args: []string{"logs", "acme-ai-xyz"}}
	got, ns, injected := repairNamespace(s, "acme-dev")
	if !injected || ns != "acme-dev" {
		t.Fatalf("expected injection, got injected=%v ns=%q", injected, ns)
	}
	if namespaceOf(got.Args); !strings.Contains(strings.Join(got.Args, " "), "-n acme-dev") {
		t.Fatalf("namespace not attached: %v", got.Args)
	}

	// already has -n → no change
	s2 := llm.Step{Command: "kubectl", Args: []string{"get", "pods", "-n", "x"}}
	if _, _, inj := repairNamespace(s2, "acme-dev"); inj {
		t.Fatal("should not inject when -n already present")
	}

	// cluster-scoped (get nodes) → no change
	s3 := llm.Step{Command: "kubectl", Args: []string{"get", "nodes"}}
	if _, _, inj := repairNamespace(s3, "acme-dev"); inj {
		t.Fatal("should not inject for cluster-scoped resources")
	}

	// -A present → no change
	s4 := llm.Step{Command: "kubectl", Args: []string{"get", "pods", "-A"}}
	if _, _, inj := repairNamespace(s4, "acme-dev"); inj {
		t.Fatal("should not inject when -A present")
	}

	// non-kubectl → no change
	s5 := llm.Step{Command: "systemctl", Args: []string{"status", "nginx"}}
	if _, _, inj := repairNamespace(s5, "acme-dev"); inj {
		t.Fatal("should not touch non-kubectl commands")
	}
}

func TestPodHealthSummary(t *testing.T) {
	// Real-shape table: all Running, but two have restarts (one with 9).
	table := `NAME                                   READY   STATUS    RESTARTS        AGE
acme-admin-7b6cb596d7-ll9sj         1/1     Running   0               6d6h
acme-ai-75c8b74f65-tmm25            1/1     Running   1 (24h ago)     6d5h
acme-compute-api-7d6c454b89-xwdlt   1/1     Running   9 (11h ago)     6d6h
acme-worker-6dbdffdf5-xmm7x         1/1     Running   0               6d6h`
	s := podHealthSummary(table)
	if !strings.Contains(s, "6 pods") && !strings.Contains(s, "4 pods") {
		t.Fatalf("missing pod count: %s", s)
	}
	if !strings.Contains(s, "All pods are Running/Ready") {
		t.Fatalf("should report all running: %s", s)
	}
	if !strings.Contains(s, "acme-compute-api") || !strings.Contains(s, "=9") {
		t.Fatalf("should surface the 9-restart pod: %s", s)
	}
}

func TestPodHealthSummaryDetectsFailing(t *testing.T) {
	table := `NAME      READY   STATUS             RESTARTS   AGE
web-1     0/1     CrashLoopBackOff   5          3m
api-2     1/1     Running            0          2d`
	s := podHealthSummary(table)
	if !strings.Contains(s, "FAILING") || !strings.Contains(s, "web-1") {
		t.Fatalf("should flag the failing pod: %s", s)
	}
}

func TestPodHealthSummaryAllHealthyConcludes(t *testing.T) {
	table := `NAME    READY   STATUS    RESTARTS   AGE
a-1     1/1     Running   0          2d
a-2     1/1     Running   0          2d`
	s := podHealthSummary(table)
	if !strings.Contains(s, "NO ERRORS") {
		t.Fatalf("all-healthy should say NO ERRORS: %s", s)
	}
}

func TestPodHealthSummaryNotAPodTable(t *testing.T) {
	if podHealthSummary("just some random text\nwith no columns") != "" {
		t.Fatal("should return empty for non-pod output")
	}
}

func TestLogErrorSummary(t *testing.T) {
	// Mimic the real run: tons of noise + a few distinct errors, repeated.
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString(`{"event":"GET /health 200","level":"info","request_id":"abc` + strconv.Itoa(i) + `"}` + "\n")
	}
	b.WriteString(`{"event":"RateLimitError: DeepSeek-V4-Pro rate limit exceeded","level":"warning","request_id":"x1"}` + "\n")
	b.WriteString(`{"event":"RateLimitError: DeepSeek-V4-Pro rate limit exceeded","level":"warning","request_id":"x2"}` + "\n")
	b.WriteString(`{"event":"BadRequestError: string too long. Expected 10485760 got 13791271","level":"error","request_id":"y1"}` + "\n")

	s := logErrorSummary(b.String())
	if !strings.Contains(s, "LOG ANALYSIS") {
		t.Fatalf("missing header: %s", s)
	}
	if !strings.Contains(s, "RateLimitError") || !strings.Contains(s, "BadRequestError") {
		t.Fatalf("should surface the real errors: %s", s)
	}
	// The two identical RateLimitError lines must collapse to one distinct entry.
	if strings.Count(s, "RateLimitError") != 1 {
		t.Fatalf("duplicate errors not de-duplicated: %s", s)
	}
}

func TestLogErrorSummaryClean(t *testing.T) {
	s := logErrorSummary("info: started\ninfo: serving\ninfo: ok")
	if !strings.Contains(s, "NO error") {
		t.Fatalf("clean logs should report no errors: %s", s)
	}
}

func TestMaybeLimitLogTail(t *testing.T) {
	got, ok := maybeLimitLogTail(llmStep("kubectl", "logs", "mypod", "-n", "acme"))
	if !ok || !strings.Contains(strings.Join(got.Args, " "), "--tail=500") {
		t.Fatalf("should add --tail: %v", got.Args)
	}
	// already has --tail → unchanged
	if _, ok := maybeLimitLogTail(llmStep("kubectl", "logs", "mypod", "--tail=10")); ok {
		t.Fatal("should not add --tail twice")
	}
	// not a logs command → unchanged
	if _, ok := maybeLimitLogTail(llmStep("kubectl", "get", "pods")); ok {
		t.Fatal("should only touch logs")
	}
}

func TestNamespaceOf(t *testing.T) {
	if ns, ok := namespaceOf([]string{"get", "pods", "-n", "prod"}); !ok || ns != "prod" {
		t.Fatalf("got %q ok=%v", ns, ok)
	}
	if ns, ok := namespaceOf([]string{"get", "pods", "--namespace=staging"}); !ok || ns != "staging" {
		t.Fatalf("got %q ok=%v", ns, ok)
	}
	if _, ok := namespaceOf([]string{"get", "pods"}); ok {
		t.Fatal("should report no namespace")
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
