package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// prompt.go is the Claude-Code / Gemini-CLI-style interactive input: a single-line editor
// with a live completion popup — "/" opens the command palette, "@" opens the entity
// picker (cartridges, namespaces, workloads). Keys: Tab accepts the highlighted
// suggestion, ↑/↓ navigate, Esc closes the popup, Enter submits, Ctrl-D exits.
//
// It runs as a short-lived Bubble Tea program per prompt and returns the typed line, so it
// composes with the existing scrolling output + approval gate (no full-screen takeover).

var (
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63"))
	descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type promptModel struct {
	ti          textinput.Model
	src         Sources
	sugg        []Suggestion
	replaceFrom int
	sel         int
	show        bool
	eof         bool
}

func newPromptModel(promptStr string, src Sources) promptModel {
	ti := textinput.New()
	ti.Prompt = promptStr
	ti.Focus()
	return promptModel{ti: ti, src: src}
}

func (m promptModel) Init() tea.Cmd { return textinput.Blink }

func (m *promptModel) refresh() {
	before := m.ti.Value()
	if p := m.ti.Position(); p >= 0 && p <= len(before) {
		before = before[:p]
	}
	m.sugg, m.replaceFrom = Complete(before, m.src)
	m.show = len(m.sugg) > 0
	if m.sel >= len(m.sugg) {
		m.sel = 0
	}
}

func (m *promptModel) accept() {
	if !m.show || len(m.sugg) == 0 {
		return
	}
	val := m.ti.Value()
	pos := m.ti.Position()
	if pos > len(val) {
		pos = len(val)
	}
	ins := m.sugg[m.sel].Value
	m.ti.SetValue(val[:m.replaceFrom] + ins + val[pos:])
	m.ti.SetCursor(m.replaceFrom + len(ins))
	m.show = false
	m.sel = 0
}

func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.Type {
		case tea.KeyCtrlD:
			m.eof = true
			m.show = false
			return m, tea.Quit
		case tea.KeyCtrlC:
			m.ti.SetValue("") // cancel the current line
			m.show = false
			return m, tea.Quit
		case tea.KeyEnter:
			m.show = false
			return m, tea.Quit
		case tea.KeyTab:
			if m.show {
				m.accept()
				return m, nil
			}
		case tea.KeyEsc:
			if m.show {
				m.show = false
				return m, nil
			}
		case tea.KeyUp:
			if m.show {
				m.sel = (m.sel - 1 + len(m.sugg)) % len(m.sugg)
				return m, nil
			}
		case tea.KeyDown:
			if m.show {
				m.sel = (m.sel + 1) % len(m.sugg)
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	m.refresh()
	return m, cmd
}

func (m promptModel) View() string {
	var b strings.Builder
	b.WriteString(m.ti.View())
	if m.show {
		const max = 8
		b.WriteString("\n")
		for i, s := range m.sugg {
			if i >= max {
				fmt.Fprintf(&b, "  %s\n", hintStyle.Render(fmt.Sprintf("…and %d more (keep typing)", len(m.sugg)-max)))
				break
			}
			line := s.Label
			if s.Desc != "" {
				line += "  " + descStyle.Render(s.Desc)
			}
			if i == m.sel {
				b.WriteString("▸ " + selStyle.Render(s.Label))
				if s.Desc != "" {
					b.WriteString("  " + descStyle.Render(s.Desc))
				}
				b.WriteString("\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString(hintStyle.Render("  Tab accept · ↑↓ move · Esc close · Enter submit"))
	}
	return b.String()
}

// Prompt shows the interactive editor and returns the submitted line. eof is true when the
// user pressed Ctrl-D (the shell should exit); a Ctrl-C returns an empty line.
func Prompt(promptStr string, src Sources) (line string, eof bool, err error) {
	res, err := tea.NewProgram(newPromptModel(promptStr, src)).Run()
	if err != nil {
		return "", false, err
	}
	m := res.(promptModel)
	return strings.TrimSpace(m.ti.Value()), m.eof, nil
}
