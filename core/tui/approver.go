package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ZentienceLabs/sahayak-cli/core/agent"
	"github.com/ZentienceLabs/sahayak-cli/core/exec"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
)

// Approver is the Bubble Tea approval gate. It satisfies agent.Approver, so the
// agent loop is unchanged from Phase 1 — only the presentation differs. Each step
// runs a short-lived inline program that renders a risk-colored card and captures
// one decision (approve / edit / reject / skip).
type Approver struct{}

// NewApprover returns a TUI-backed approval gate.
func NewApprover() *Approver { return &Approver{} }

// Review implements agent.Approver.
func (a *Approver) Review(step llm.Step, risk exec.Risk, index, total int) (agent.Decision, llm.Step, error) {
	m := newReviewModel(step, risk, index, total)
	// Inline (not alt-screen) so each approved command + its output accumulates
	// naturally in the scrollback, matching the line-mode experience.
	out, err := tea.NewProgram(m).Run()
	if err != nil {
		return agent.Reject, step, err
	}
	rm := out.(reviewModel)
	if rm.aborted {
		// Treat Ctrl-C / q / esc as the safe choice: reject the plan.
		return agent.Reject, step, nil
	}
	return rm.decision, rm.finalStep, nil
}

type gateState int

const (
	stateDecide gateState = iota
	stateEdit
)

type reviewModel struct {
	step  llm.Step
	risk  exec.Risk
	index int
	total int

	state gateState
	input textinput.Model

	decision  agent.Decision
	finalStep llm.Step
	done      bool
	aborted   bool
}

func newReviewModel(step llm.Step, risk exec.Risk, index, total int) reviewModel {
	ti := textinput.New()
	ti.Prompt = "$ "
	ti.SetValue(commandLine(step))
	ti.CharLimit = 0
	ti.Width = 60
	return reviewModel{
		step:      step,
		risk:      risk,
		index:     index,
		total:     total,
		state:     stateDecide,
		input:     ti,
		finalStep: step,
	}
}

func (m reviewModel) Init() tea.Cmd { return nil }

func (m reviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.state == stateEdit {
		switch key.Type {
		case tea.KeyEnter:
			edited := parseCommandLine(m.input.Value(), m.step.Explanation)
			m.finalStep = edited
			m.decision = agent.Edit
			m.done = true
			return m, tea.Quit
		case tea.KeyEsc:
			m.state = stateDecide
			m.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch strings.ToLower(key.String()) {
	case "a", "y", "enter":
		m.decision = agent.Approve
		m.finalStep = m.step
		m.done = true
		return m, tea.Quit
	case "e":
		m.state = stateEdit
		return m, m.input.Focus()
	case "s":
		m.decision = agent.Skip
		m.done = true
		return m, tea.Quit
	case "r", "n":
		m.decision = agent.Reject
		m.done = true
		return m, tea.Quit
	case "q", "ctrl+c", "esc":
		m.aborted = true
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m reviewModel) View() string {
	accent := riskColor(m.risk)

	// Once decided, collapse to a compact one-line record left in the scrollback.
	if m.done {
		return styleWhisper.Render("  "+decisionLabel(m)) + "\n"
	}

	var b strings.Builder
	title := styleTitle.Render("Sahayak") + styleWhisper.Render(fmt.Sprintf("  ·  step %d/%d", m.index+1, m.total))
	b.WriteString(title + "\n\n")

	if m.state == stateEdit {
		b.WriteString(styleWhisper.Render("edit the command, then Enter to run (Esc to cancel):") + "\n")
		b.WriteString(m.input.View() + "\n")
	} else {
		b.WriteString(styleCommand.Render("$ "+commandLine(m.step)) + "\n\n")
		b.WriteString(styleWhisper.Render("why  ") + wrap(m.step.Explanation, 60) + "\n")
		b.WriteString(styleWhisper.Render("risk ") + riskBadge(m.risk) + "\n\n")
		b.WriteString(keyHints())
	}

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 2).
		Render(b.String())
	return card + "\n"
}

func keyHints() string {
	parts := []string{
		styleKeyHint.Render("[a]") + "pprove",
		styleKeyHint.Render("[e]") + "dit",
		styleKeyHint.Render("[r]") + "eject",
		styleKeyHint.Render("[s]") + "kip",
	}
	return strings.Join(parts, "   ")
}

func decisionLabel(m reviewModel) string {
	if m.aborted {
		return "✗ rejected (aborted)"
	}
	switch m.decision {
	case agent.Approve:
		return "✓ approved: $ " + commandLine(m.step)
	case agent.Edit:
		return "✎ edited:   $ " + commandLine(m.finalStep)
	case agent.Skip:
		return "↷ skipped:  $ " + commandLine(m.step)
	default:
		return "✗ rejected: $ " + commandLine(m.step)
	}
}

// commandLine renders a step as a single shell-ish line for display/editing.
func commandLine(s llm.Step) string {
	return strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
}

// parseCommandLine splits an edited line back into a Step. Re-classification of
// risk happens in the agent before the edited step runs.
func parseCommandLine(line, explanation string) llm.Step {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return llm.Step{Explanation: explanation}
	}
	return llm.Step{
		Command:     fields[0],
		Args:        fields[1:],
		Explanation: explanation + " (edited by operator)",
	}
}

// wrap soft-wraps text to width columns for the card body.
func wrap(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}
