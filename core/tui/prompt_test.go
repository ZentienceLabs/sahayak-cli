package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSanitizePaste(t *testing.T) {
	cases := map[string]string{
		"hello":               "hello",
		"a\nb":                "a b",
		"a\r\nb\tc":           "a b c",
		"line1\nline2\nline3": "line1 line2 line3",
		"trailing\n":          "trailing",
	}
	for in, want := range cases {
		if got := sanitizePaste(in); got != want {
			t.Errorf("sanitizePaste(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHistoryNavigation(t *testing.T) {
	m := newPromptModel("> ", Sources{}, []string{"first cmd", "second cmd"})
	if m.histPos != 2 {
		t.Fatalf("histPos should start at len(hist)=2, got %d", m.histPos)
	}
	// type a draft
	m.ti.SetValue("draft in progress")

	m.histPrev() // → newest history entry, draft stashed
	if m.ti.Value() != "second cmd" {
		t.Fatalf("histPrev should recall 'second cmd', got %q", m.ti.Value())
	}
	m.histPrev() // → older
	if m.ti.Value() != "first cmd" {
		t.Fatalf("histPrev should recall 'first cmd', got %q", m.ti.Value())
	}
	m.histPrev() // → clamp at oldest
	if m.ti.Value() != "first cmd" {
		t.Errorf("histPrev past oldest should stay, got %q", m.ti.Value())
	}
	m.histNext() // → 'second cmd'
	m.histNext() // → back to the stashed draft
	if m.ti.Value() != "draft in progress" {
		t.Errorf("histNext back to live should restore the draft, got %q", m.ti.Value())
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
