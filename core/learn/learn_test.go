package learn

import (
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Record(Event{Kind: "adhoc", Command: "kubectl", Args: []string{"get", "ns"}, Success: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(Event{Kind: "missed", Request: "do a barrel roll"}); err != nil {
		t.Fatal(err)
	}
	evs, err := s.Events()
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Command != "kubectl" || evs[1].Request != "do a barrel roll" {
		t.Fatalf("round trip wrong: %+v", evs)
	}
}

func TestSuggestPromotesRepeatedAdhoc(t *testing.T) {
	events := []Event{
		{Kind: "adhoc", Command: "kubectl", Args: []string{"logs", "deploy/web", "-n", "dev"}, Success: true},
		{Kind: "adhoc", Command: "kubectl", Args: []string{"logs", "deploy/api", "-n", "demo"}, Success: true},
		{Kind: "adhoc", Command: "kubectl", Args: []string{"get", "ns"}, Success: true}, // only once → no suggestion
	}
	sugs := Suggest(events)
	var promote *Suggestion
	for i := range sugs {
		if sugs[i].Kind == "promote-template" {
			promote = &sugs[i]
		}
	}
	if promote == nil {
		t.Fatal("expected a promote-template suggestion for the repeated `kubectl logs deploy/...`")
	}
	if promote.Count != 2 {
		t.Errorf("count = %d, want 2 (the two logs commands grouped)", promote.Count)
	}
}

func TestSuggestFlagsFailingIntent(t *testing.T) {
	events := []Event{
		{Kind: "routed", Cartridge: "k8s", Intent: "verifyenv", Command: "kubectl", Args: []string{"exec"}, Success: false},
		{Kind: "routed", Cartridge: "k8s", Intent: "verifyenv", Command: "kubectl", Args: []string{"exec"}, Success: false},
		{Kind: "routed", Cartridge: "k8s", Intent: "list", Success: true}, // success → not flagged
	}
	sugs := Suggest(events)
	found := false
	for _, s := range sugs {
		if s.Kind == "fix-template" && strings.Contains(s.Detail, "k8s.verifyenv") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a fix-template suggestion for the repeatedly-failing k8s.verifyenv")
	}
}

func TestSuggestCoversGaps(t *testing.T) {
	events := []Event{
		{Kind: "missed", Request: "frobnicate the widget"},
		{Kind: "missed", Request: "reticulate splines"},
	}
	sugs := Suggest(events)
	found := false
	for _, s := range sugs {
		if s.Kind == "cover-gap" && s.Count == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a cover-gap suggestion, got %+v", sugs)
	}
}

func TestTopAdhocCommand(t *testing.T) {
	events := []Event{
		{Kind: "adhoc", Command: "kubectl", Args: []string{"get", "ns"}, Success: true},
		{Kind: "adhoc", Command: "kubectl", Args: []string{"get", "ns"}, Success: true},
		{Kind: "adhoc", Command: "docker", Args: []string{"ps"}, Success: true},
		{Kind: "adhoc", Command: "rm", Args: []string{"-rf", "x"}, Success: false}, // failures ignored
	}
	cmd, args, ok := TopAdhocCommand(events)
	if !ok || cmd != "kubectl" || len(args) != 2 || args[0] != "get" {
		t.Fatalf("TopAdhocCommand = %q %v ok=%v, want kubectl [get ns]", cmd, args, ok)
	}
	if _, _, ok := TopAdhocCommand(nil); ok {
		t.Error("no events → ok=false")
	}
}

func TestSuggestIgnoresOneOffs(t *testing.T) {
	// A single success and a single miss should produce nothing (below MinOccurrences).
	sugs := Suggest([]Event{
		{Kind: "adhoc", Command: "ls", Success: true},
		{Kind: "missed", Request: "x"},
	})
	if len(sugs) != 0 {
		t.Fatalf("one-offs should not suggest, got %+v", sugs)
	}
}
