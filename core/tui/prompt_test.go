package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHistoryNavigation(t *testing.T) {
	m := newPromptModel("> ", Sources{}, []string{"first cmd", "second cmd"}, "")
	if m.histPos != 2 {
		t.Fatalf("histPos should start at len(hist)=2, got %d", m.histPos)
	}
	m.ta.SetValue("draft in progress")

	m.histPrev() // → newest history entry, draft stashed
	if m.ta.Value() != "second cmd" {
		t.Fatalf("histPrev should recall 'second cmd', got %q", m.ta.Value())
	}
	m.histPrev() // → older
	if m.ta.Value() != "first cmd" {
		t.Fatalf("histPrev should recall 'first cmd', got %q", m.ta.Value())
	}
	m.histPrev() // → clamp at oldest
	if m.ta.Value() != "first cmd" {
		t.Errorf("histPrev past oldest should stay, got %q", m.ta.Value())
	}
	m.histNext() // → 'second cmd'
	m.histNext() // → back to the stashed draft
	if m.ta.Value() != "draft in progress" {
		t.Errorf("histNext back to live should restore the draft, got %q", m.ta.Value())
	}
}

func TestMultilineSkipsCompletion(t *testing.T) {
	// On a multi-line value the "/"/"@" popup is suppressed (we complete single-line only).
	m := newPromptModel("> ", Sources{Cartridges: []string{"k8s"}}, nil, "")
	m.ta.SetValue("line one\n@k")
	m.refresh()
	if m.show {
		t.Error("completion should be suppressed for multi-line input")
	}
}

func TestSelectEnterChooses(t *testing.T) {
	m := selectModel{options: []string{"a", "b", "c"}, cursor: 0, current: "a"}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	nm, _ = nm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sm := nm.(selectModel)
	if !sm.chosen || sm.options[sm.cursor] != "b" {
		t.Fatalf("expected to choose 'b', chosen=%v cursor=%d", sm.chosen, sm.cursor)
	}
}
