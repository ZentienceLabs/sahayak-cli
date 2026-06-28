package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ZentienceLabs/sahayak-cli/core/agent"
	"github.com/ZentienceLabs/sahayak-cli/core/exec"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
)

func step() llm.Step {
	return llm.Step{Command: "systemctl", Args: []string{"reload", "nginx"}, Explanation: "apply config"}
}

// send pushes a key message through the model and returns the updated model.
func send(m reviewModel, msg tea.Msg) reviewModel {
	next, _ := m.Update(msg)
	return next.(reviewModel)
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestReviewDecisions(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
		want agent.Decision
	}{
		{"approve", runeKey('a'), agent.Approve},
		{"approve-yes", runeKey('y'), agent.Approve},
		{"skip", runeKey('s'), agent.Skip},
		{"reject", runeKey('r'), agent.Reject},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := send(newReviewModel(step(), exec.Mutating, 0, 1), c.key)
			if !m.done || m.decision != c.want {
				t.Fatalf("got decision=%v done=%v, want %v", m.decision, m.done, c.want)
			}
		})
	}
}

func TestReviewAbort(t *testing.T) {
	m := send(newReviewModel(step(), exec.Mutating, 0, 1), tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.aborted {
		t.Fatalf("ctrl+c should abort, got %+v", m)
	}
}

func TestReviewEditFlow(t *testing.T) {
	m := newReviewModel(step(), exec.Mutating, 0, 1)
	// Enter edit mode.
	m = send(m, runeKey('e'))
	if m.state != stateEdit {
		t.Fatalf("expected edit state, got %v", m.state)
	}
	// Operator rewrites the command, then confirms with Enter.
	m.input.SetValue("systemctl restart nginx")
	m = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.decision != agent.Edit || !m.done {
		t.Fatalf("expected Edit decision, got %v done=%v", m.decision, m.done)
	}
	if m.finalStep.Command != "systemctl" || len(m.finalStep.Args) != 2 || m.finalStep.Args[1] != "nginx" {
		t.Fatalf("edited step not parsed correctly: %+v", m.finalStep)
	}
}

func TestReviewEditCancel(t *testing.T) {
	m := newReviewModel(step(), exec.Mutating, 0, 1)
	m = send(m, runeKey('e'))
	m = send(m, tea.KeyMsg{Type: tea.KeyEsc}) // cancel edit
	if m.state != stateDecide || m.done {
		t.Fatalf("esc should return to decide state without finishing, got state=%v done=%v", m.state, m.done)
	}
}

// Compile-time assertion that the TUI Approver satisfies the agent interface.
var _ agent.Approver = (*Approver)(nil)
