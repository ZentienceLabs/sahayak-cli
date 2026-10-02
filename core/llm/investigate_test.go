package llm

import "testing"

func TestParseNextAction_Action(t *testing.T) {
	raw := `{"thought":"need to find the namespace","action":{"command":"kubectl get namespaces","args":[],"explanation":"list ns"},"done":false}`
	na, err := ParseNextAction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if na.Done || na.Action == nil {
		t.Fatalf("expected an action, got %+v", na)
	}
	// Full-line command must be normalized to command+args.
	if na.Action.Command != "kubectl" || len(na.Action.Args) != 2 || na.Action.Args[0] != "get" {
		t.Fatalf("action not normalized: %+v", na.Action)
	}
}

func TestParseNextAction_Done(t *testing.T) {
	raw := "```json\n{\"thought\":\"found it\",\"done\":true,\"final_answer\":\"namespace acme-dev has 2 failing pods\"}\n```"
	na, err := ParseNextAction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !na.Done || na.Action != nil || na.FinalAnswer == "" {
		t.Fatalf("expected done with final answer, got %+v", na)
	}
}

func TestParseNextAction_EmptyCommandBecomesDone(t *testing.T) {
	raw := `{"thought":"nothing to do","action":{"command":"","args":[]},"done":false}`
	na, err := ParseNextAction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !na.Done || na.Action != nil {
		t.Fatalf("blank command should become done, got %+v", na)
	}
}
