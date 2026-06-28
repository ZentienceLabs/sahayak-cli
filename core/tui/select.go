package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Select is a small arrow-key picker (used for choosing the model in the rich shell, so
// the shell never mixes bufio reads with Bubble Tea on stdin — important on Windows).
// ↑/↓ or j/k move, Enter chooses, Esc/Ctrl-C cancels (keeping the current value).

type selectModel struct {
	title   string
	options []string
	cursor  int
	current string
	chosen  bool
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.Type {
	case tea.KeyCtrlC, tea.KeyEsc, tea.KeyCtrlD:
		return m, tea.Quit
	case tea.KeyEnter:
		m.chosen = true
		return m, tea.Quit
	case tea.KeyUp:
		m.cursor = (m.cursor - 1 + len(m.options)) % len(m.options)
	case tea.KeyDown:
		m.cursor = (m.cursor + 1) % len(m.options)
	case tea.KeyRunes:
		switch string(k.Runes) {
		case "k":
			m.cursor = (m.cursor - 1 + len(m.options)) % len(m.options)
		case "j":
			m.cursor = (m.cursor + 1) % len(m.options)
		}
	}
	return m, nil
}

func (m selectModel) View() string {
	var b strings.Builder
	b.WriteString(m.title + "\n")
	for i, o := range m.options {
		mark := ""
		if o == m.current {
			mark = descStyle.Render("  (current)")
		}
		if i == m.cursor {
			b.WriteString("▸ " + selStyle.Render(o) + mark + "\n")
		} else {
			b.WriteString("  " + o + mark + "\n")
		}
	}
	b.WriteString(hintStyle.Render("↑↓ move · Enter choose · Esc keep current"))
	return b.String()
}

// Select shows the picker and returns the chosen option. ok is false if cancelled (the
// caller should keep `current`). Falls back via err if no terminal.
func Select(title string, options []string, current string) (choice string, ok bool, err error) {
	cursor := 0
	for i, o := range options {
		if o == current {
			cursor = i
			break
		}
	}
	res, err := tea.NewProgram(selectModel{title: title, options: options, cursor: cursor, current: current}).Run()
	if err != nil {
		return current, false, err
	}
	m := res.(selectModel)
	if !m.chosen {
		return current, false, nil
	}
	return m.options[m.cursor], true, nil
}
