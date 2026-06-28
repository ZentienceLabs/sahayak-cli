package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// prompt.go is the Claude-Code / Gemini-CLI-style interactive input: a bordered, multi-line
// editor with a status line beneath it and a live completion popup. "/" opens the command
// palette (incl. subcommands), "@" opens the entity picker (cartridges, namespaces,
// workloads).
//
// Keys: Enter submits · Ctrl-J inserts a newline (Shift-Enter where the terminal supports
// it) · ←/→/Home/End and word-motion move the cursor · ↑/↓ move between lines (or the popup
// when open) · Ctrl-P/Ctrl-N recall history · Tab accepts a suggestion · Esc closes the
// popup · Ctrl-D exits · Ctrl-C clears.
//
// It runs as a short-lived Bubble Tea program per prompt and returns the typed line(s), so
// it composes with the scrolling output + approval gate. Multi-line pastes are kept as-is
// (the editor is multi-line). "Pinned to the bottom" is inline: the box renders at the end
// of the current output; a true always-pinned bottom needs the full-screen mode.

var (
	selStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("63"))
	descStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	boxStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
)

type promptModel struct {
	ta          textarea.Model
	src         Sources
	status      string
	width       int
	sugg        []Suggestion
	replaceFrom int
	sel         int
	show        bool
	eof         bool

	hist    []string
	histPos int
	draft   string
}

func newPromptModel(promptStr string, src Sources, history []string, status string) promptModel {
	ta := textarea.New()
	ta.Prompt = promptStr
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.SetWidth(80)
	// Enter submits (handled in Update); Ctrl-J (and shift+enter where available) makes a newline.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "shift+enter"))
	ta.Focus()
	return promptModel{ta: ta, src: src, hist: history, histPos: len(history), status: status, width: 80}
}

func (m promptModel) Init() tea.Cmd { return textarea.Blink }

// beforeCursor returns the text from the start of the (single) line to the cursor, and ok
// only when the input is single-line (we complete "/" and "@" on a single line).
func (m *promptModel) beforeCursor() (string, bool) {
	v := m.ta.Value()
	if strings.Contains(v, "\n") {
		return "", false
	}
	col := m.ta.LineInfo().ColumnOffset
	if col > len(v) {
		col = len(v)
	}
	return v[:col], true
}

func (m *promptModel) refresh() {
	before, ok := m.beforeCursor()
	if !ok {
		m.show, m.sugg = false, nil
		return
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
	v := m.ta.Value()
	col := m.ta.LineInfo().ColumnOffset
	if col > len(v) {
		col = len(v)
	}
	ins := m.sugg[m.sel].Value
	m.ta.SetValue(v[:m.replaceFrom] + ins + v[col:])
	m.ta.CursorEnd()
	m.show, m.sel = false, 0
}

func (m *promptModel) histSet(s string) {
	m.ta.SetValue(s)
	m.ta.CursorEnd()
}
func (m *promptModel) histPrev() {
	if len(m.hist) == 0 || m.histPos == 0 {
		return
	}
	if m.histPos == len(m.hist) {
		m.draft = m.ta.Value()
	}
	m.histPos--
	m.histSet(m.hist[m.histPos])
}
func (m *promptModel) histNext() {
	if m.histPos >= len(m.hist) {
		return
	}
	m.histPos++
	if m.histPos == len(m.hist) {
		m.histSet(m.draft)
	} else {
		m.histSet(m.hist[m.histPos])
	}
}

func (m *promptModel) syncSize() {
	lines := strings.Count(m.ta.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > 6 {
		lines = 6
	}
	m.ta.SetHeight(lines)
	if m.width > 8 {
		m.ta.SetWidth(m.width - 4) // room for the border + padding
	}
}

func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.syncSize()
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlD:
			m.eof = true
			return m, tea.Quit
		case tea.KeyCtrlC:
			m.ta.SetValue("")
			return m, tea.Quit
		case tea.KeyEnter: // submit (newline is Ctrl-J)
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
		case tea.KeyCtrlP:
			m.histPrev()
			m.refresh()
			m.syncSize()
			return m, nil
		case tea.KeyCtrlN:
			m.histNext()
			m.refresh()
			m.syncSize()
			return m, nil
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
	m.ta, cmd = m.ta.Update(msg)
	m.refresh()
	m.syncSize()
	return m, cmd
}

func (m promptModel) renderPopup() string {
	const max = 8
	var b strings.Builder
	for i, s := range m.sugg {
		if i >= max {
			fmt.Fprintf(&b, "  %s\n", hintStyle.Render(fmt.Sprintf("…and %d more (keep typing)", len(m.sugg)-max)))
			break
		}
		if i == m.sel {
			b.WriteString("▸ " + selStyle.Render(s.Label))
		} else {
			b.WriteString("  " + s.Label)
		}
		if s.Desc != "" {
			b.WriteString("  " + descStyle.Render(s.Desc))
		}
		b.WriteString("\n")
	}
	b.WriteString(hintStyle.Render("  Tab accept · ↑↓ move · Esc close"))
	return b.String()
}

func (m promptModel) View() string {
	var b strings.Builder
	if m.show {
		b.WriteString(m.renderPopup() + "\n")
	}
	b.WriteString(boxStyle.Render(m.ta.View()) + "\n")
	status := m.status
	if status == "" {
		status = "/ commands · @ entities"
	}
	b.WriteString(hintStyle.Render(status + "   ·   Enter send · Ctrl-J newline · Ctrl-D exit"))
	return b.String()
}

// Prompt shows the bordered editor and returns the submitted text. history feeds Ctrl-P/N
// recall; status is shown on the line below the box. eof is true on Ctrl-D.
func Prompt(promptStr string, src Sources, history []string, status string) (line string, eof bool, err error) {
	res, err := tea.NewProgram(newPromptModel(promptStr, src, history, status)).Run()
	if err != nil {
		return "", false, err
	}
	m := res.(promptModel)
	return strings.TrimSpace(m.ta.Value()), m.eof, nil
}
