package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type historyModel struct {
	hist     []string
	filtered []string
	ti       textinput.Model
	cursor   int
	chosen   string
	done     bool
}

func HistorySearch(history []string) string {
	ti := textinput.New()
	ti.Prompt = "(reverse-i-search)`"
	ti.Placeholder = "..."
	ti.Focus()

	// Reverse the history for display (newest first)
	var rev []string
	for i := len(history) - 1; i >= 0; i-- {
		rev = append(rev, history[i])
	}

	m := historyModel{
		hist:     rev,
		filtered: rev,
		ti:       ti,
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return ""
	}
	fm, ok := finalModel.(historyModel)
	if !ok || !fm.done || fm.chosen == "" {
		return ""
	}
	return fm.chosen
}

func (m historyModel) Init() tea.Cmd { return textinput.Blink }

func (m historyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc, tea.KeyCtrlG:
			m.done = true
			return m, tea.Quit
		case tea.KeyEnter:
			if len(m.filtered) > 0 {
				m.chosen = m.filtered[m.cursor]
			}
			m.done = true
			return m, tea.Quit
		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown, tea.KeyCtrlN:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		default:
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			m.filter()
			return m, cmd
		}
	}
	return m, nil
}

func (m *historyModel) filter() {
	q := strings.ToLower(m.ti.Value())
	var out []string
	for _, h := range m.hist {
		if q == "" || strings.Contains(strings.ToLower(h), q) {
			out = append(out, h)
		}
	}
	m.filtered = out
	m.cursor = 0
}

func (m historyModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.ti.View() + "':\n")

	max := 10
	if len(m.filtered) < max {
		max = len(m.filtered)
	}
	for i := 0; i < max; i++ {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
		if i == m.cursor {
			cursor = "> "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true)
		}
		b.WriteString(cursor + style.Render(m.filtered[i]) + "\n")
	}
	return b.String()
}
