package agent

import (
	"strings"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/llm"
)

// Reproduces the real failure: the model listed configmaps cluster-wide (the
// acme-web ones live in acme-dev/acme-demo), then wrongly concluded
// "no acme-web in the acme namespace". The guard must surface the matches.
func TestCrossCheckAbsenceSurfacesMatches(t *testing.T) {
	obs := []observation{
		{command: "kubectl get namespaces", exitOK: true,
			text: "NAME          STATUS   AGE\nacme       Active   97d\nacme-dev   Active   165d"},
		{command: "kubectl get configmaps -A", exitOK: true,
			text: "NAMESPACE     NAME                 DATA   AGE\nacme-dev   acme-web-config   24     165d\nacme-demo  acme-web-config   3      165d"},
	}
	answer := "No configmap named 'acme-web' exists in the 'acme' namespace."
	corrected, ok := crossCheckAbsence("can you provide configmap list for acme-web", answer, obs)
	if !ok {
		t.Fatal("expected the absence claim to be corrected")
	}
	if !strings.Contains(corrected, "acme-web-config") || !strings.Contains(corrected, "acme-dev") {
		t.Fatalf("correction missing the real matches:\n%s", corrected)
	}
}

// A correct health conclusion ("no errors", all pods Running) must NOT be reframed
// into "found N matches" just because the keyword appears in the healthy rows.
func TestCrossCheckSkipsHealthAbsence(t *testing.T) {
	obs := []observation{
		{command: "kubectl get pods -n acme-dev", exitOK: true,
			text: "NAME                          READY   STATUS    RESTARTS   AGE\nacme-web-58678dcd5-sq5v9   1/1     Running   0          21m\nacme-ai-694f94c4f9-hb4z2   1/1     Running   0          17h"},
	}
	answer := "All pods are Running with no errors in the acme environment."
	if _, ok := crossCheckAbsence("are there any errors in acme dev", answer, obs); ok {
		t.Fatal("must not override a correct 'no errors' health conclusion")
	}
}

func TestCrossCheckIgnoresNonAbsenceAndNoMatch(t *testing.T) {
	obs := []observation{{command: "kubectl get cm -A", exitOK: true,
		text: "NAMESPACE  NAME         DATA  AGE\nacme    other-config 1     5d"}}
	// Not an absence claim → no correction.
	if _, ok := crossCheckAbsence("list configmaps for acme-web", "Here are the configmaps: other-config", obs); ok {
		t.Fatal("should not correct a non-absence answer")
	}
	// Absence claim but genuinely nothing matches "acme-web" → leave it alone.
	if _, ok := crossCheckAbsence("list configmaps for acme-web", "No acme-web configmap was found.", obs); ok {
		t.Fatal("should not fabricate a correction when there are no real matches")
	}
}

func TestCrossCheckSkipsHeadersAndAnnotations(t *testing.T) {
	obs := []observation{{command: "kubectl get cm -A", exitOK: true,
		text: "NAMESPACE  NAME  DATA  AGE\n(no keyword/abnormal matches; showing first 25 of 88 lines)"}}
	if _, ok := crossCheckAbsence("configmaps for acme-web", "no acme-web found", obs); ok {
		t.Fatal("header/annotation lines must not count as matches")
	}
}

func TestDropRedundantNamespace(t *testing.T) {
	s := llm.Step{Command: "kubectl", Args: []string{"get", "configmaps", "-n", "acme", "--all-namespaces"}}
	cleaned, ok := dropRedundantNamespace(s)
	if !ok {
		t.Fatal("expected the contradictory -n to be dropped")
	}
	for i, a := range cleaned.Args {
		if a == "-n" || a == "acme" {
			t.Fatalf("namespace flag survived at %d: %v", i, cleaned.Args)
		}
	}
	// No -A: leave a normal namespaced command untouched.
	s2 := llm.Step{Command: "kubectl", Args: []string{"get", "configmaps", "-n", "acme"}}
	if _, ok := dropRedundantNamespace(s2); ok {
		t.Fatal("must not touch a plain namespaced command")
	}
}

// The keyword extractor must reduce "can you provide configmap list for acme-web"
// to just the distinctive name — otherwise generic words like "configmap" match
// every row of `-o name` output and truncate the real matches away.
func TestExtractKeywordsDropsNoise(t *testing.T) {
	got := extractKeywords("can you provide configmap list for acme-web")
	if len(got) != 1 || got[0] != "acme-web" {
		t.Fatalf("expected only [acme-web], got %v", got)
	}
}

// End-to-end of the fix: with only the distinctive keyword, condensing a large
// `-o name` listing keeps the acme-web rows, so the absence guard can fire.
func TestCrossCheckFiresOnDashOName(t *testing.T) {
	// Simulate the condensed observation: only acme-web lines survived filtering.
	obs := []observation{{command: "kubectl get configmap -A -o name", exitOK: true,
		text: "configmap/acme-web-config\nconfigmap/acme-web-favicon"}}
	answer := "No configmap found in the acme-web namespace. The list is empty."
	corrected, ok := crossCheckAbsence("can you provide configmap list for acme-web", answer, obs)
	if !ok {
		t.Fatal("guard should fire on the -o name observation")
	}
	if !strings.Contains(corrected, "acme-web-config") || !strings.Contains(corrected, "acme-web-favicon") {
		t.Fatalf("correction missing the configmaps: %s", corrected)
	}
}
