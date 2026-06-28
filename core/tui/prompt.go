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
// suggestion, ↑/↓ navigate the popup (or command history when it's closed), Esc closes the
// popup, Enter submits, Ctrl-D exits, Ctrl-C clears the line.
//
// It runs as a short-lived Bubble Tea program per prompt and returns the typed line, so it
// composes with the scrolling output + approval gate (no full-screen takeover). Hardened
// for real terminals: multi-line pastes are flattened (single-line editor), window resizes
// adjust width, and Ctrl-D/Ctrl-C behave like a shell.

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

	hist    []string // command history, oldest→newest
	histPos int      // index into hist; == len(hist) means the live draft
	draft   string   // the in-progress line, stashed while browsing history
}

func newPromptModel(promptStr string, src Sources, history []string) promptModel {
	ti := textinput.New()
	ti.Prompt = promptStr
	ti.Focus()
	return promptModel{ti: ti, src: src, hist: history, histPos: len(history)}
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

// insertSanitized inserts pasted text at the cursor, flattening newlines/tabs to spaces so
// a multi-line paste can't break the single-line editor or submit early.
func (m *promptModel) insertSanitized(s string) {
	s = sanitizePaste(s)
	val := m.ti.Value()
	pos := m.ti.Position()
	if pos > len(val) {
		pos = len(val)
	}
	m.ti.SetValue(val[:pos] + s + val[pos:])
	m.ti.SetCursor(pos + len(s))
}

func sanitizePaste(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ").Replace(s)
	return strings.TrimRight(s, " ")
}

func (m *promptModel) histPrev() {
	if len(m.hist) == 0 || m.histPos == 0 {
		return
	}
	if m.histPos == len(m.hist) {
		m.draft = m.ti.Value() // stash the live line before browsing
	}
	m.histPos--
	m.ti.SetValue(m.hist[m.histPos])
	m.ti.SetCursor(len(m.ti.Value()))
}

func (m *promptModel) histNext() {
	if m.histPos >= len(m.hist) {
		return
	}
	m.histPos++
	if m.histPos == len(m.hist) {
		m.ti.SetValue(m.draft)
	} else {
		m.ti.SetValue(m.hist[m.histPos])
	}
	m.ti.SetCursor(len(m.ti.Value()))
}

func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if w := msg.Width - 14; w > 10 {
			m.ti.Width = w
		}
		return m, nil
	case tea.KeyMsg:
		if msg.Paste { // bracketed paste arrives as one message
			m.insertSanitized(string(msg.Runes))
			m.refresh()
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlD:
			m.eof = true
			m.show = false
			return m, tea.Quit
		case tea.KeyCtrlC:
			m.ti.SetValue("")
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
			} else {
				m.histPrev()
			}
			return m, nil
		case tea.KeyDown:
			if m.show {
				m.sel = (m.sel + 1) % len(m.sugg)
			} else {
				m.histNext()
			}
			return m, nil
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
		b.WriteString(hintStyle.Render("  Tab accept · ↑↓ move · Esc close · Enter submit"))
	}
	return b.String()
}

// Prompt shows the interactive editor and returns the submitted line. history feeds ↑/↓
// recall. eof is true when the user pressed Ctrl-D (the shell should exit); Ctrl-C returns
// an empty line. On any non-terminal/setup error it returns err so the caller can fall back
// to a plain readline.
func Prompt(promptStr string, src Sources, history []string) (line string, eof bool, err error) {
	res, err := tea.NewProgram(newPromptModel(promptStr, src, history)).Run()
	if err != nil {
		return "", false, err
	}
	m := res.(promptModel)
	return strings.TrimSpace(m.ti.Value()), m.eof, nil
}
