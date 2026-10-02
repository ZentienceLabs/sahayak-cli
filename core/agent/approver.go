package agent

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/exec"
	"github.com/ZentienceLabs/sahayak-cli/core/llm"
)

// Decision is the operator's choice at the approval gate.
type Decision int

const (
	// Approve runs the step as proposed.
	Approve Decision = iota
	// Edit runs an operator-modified version of the step.
	Edit
	// Reject abandons the whole plan.
	Reject
	// Skip skips this step and continues with the next.
	Skip
)

// Approver presents a step and returns the operator's decision (and, for Edit,
// the modified step). The line-based implementation here is the Phase-1 gate;
// Phase 2 swaps in a Bubble Tea TUI behind the same interface.
type Approver interface {
	Review(step llm.Step, risk exec.Risk, index, total int) (Decision, llm.Step, error)
}

// LineApprover is a robust, scriptable, SSH/CI-friendly approval gate that reads
// single-key decisions from a reader and writes prompts to a writer.
type LineApprover struct {
	In  io.Reader
	Out io.Writer

	reader *bufio.Reader
}

// NewLineApprover builds a LineApprover over the given streams (typically stdin/stdout).
func NewLineApprover(in io.Reader, out io.Writer) *LineApprover {
	return &LineApprover{In: in, Out: out, reader: bufio.NewReader(in)}
}

// Review implements Approver.
func (l *LineApprover) Review(step llm.Step, risk exec.Risk, index, total int) (Decision, llm.Step, error) {
	marker := riskMarker(risk)
	fmt.Fprintf(l.Out, "\n  Step %d/%d — %s\n", index+1, total, step.Pretty())
	fmt.Fprintf(l.Out, "    Why:  %s\n", step.Explanation)
	fmt.Fprintf(l.Out, "    Risk: %s %s\n", risk.String(), marker)
	fmt.Fprintf(l.Out, "    [a]pprove  [e]dit  [r]eject  [s]kip: ")

	line, err := l.reader.ReadString('\n')
	if err != nil && line == "" {
		return Reject, step, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "a", "approve", "y", "yes":
		return Approve, step, nil
	case "e", "edit":
		edited, err := l.editStep(step)
		if err != nil {
			return Reject, step, err
		}
		return Edit, edited, nil
	case "s", "skip":
		return Skip, step, nil
	default:
		// Default to the safe choice: reject anything not explicitly approved.
		return Reject, step, nil
	}
}

// editStep lets the operator retype the full command line. The edited line is
// re-split into command+args; it is re-classified by the caller before running.
func (l *LineApprover) editStep(step llm.Step) (llm.Step, error) {
	fmt.Fprintf(l.Out, "    edit command line: ")
	line, err := l.reader.ReadString('\n')
	if err != nil && line == "" {
		return step, err
	}
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return step, nil // empty edit keeps the original
	}
	return llm.Step{
		Command:     fields[0],
		Args:        fields[1:],
		Explanation: step.Explanation + " (edited by operator)",
	}, nil
}

func riskMarker(r exec.Risk) string {
	switch r {
	case exec.ReadOnly:
		return "✓"
	case exec.Mutating:
		return "⚠"
	case exec.Destructive:
		return "‼ DANGER"
	default:
		return "?"
	}
}
