// Package ui renders Sahayak's terminal experience: a branded header, animated
// spinners while the model thinks, color-coded steps and risk, and a boxed
// conclusion. It degrades gracefully to plain text when stdout isn't a TTY (CI,
// pipes), so automation never sees escape codes or hangs on a spinner.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Palette — kept in one place so every surface stays consistent.
var (
	cBrand = lipgloss.Color("#A78BFA")
	cDim   = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	cCmd   = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#22D3EE"}
	cOK    = lipgloss.Color("#22C55E")
	cWarn  = lipgloss.Color("#EAB308")
	cErr   = lipgloss.Color("#EF4444")

	sBrand   = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	sDim     = lipgloss.NewStyle().Foreground(cDim)
	sCmd     = lipgloss.NewStyle().Foreground(cCmd).Bold(true)
	sThought = lipgloss.NewStyle().Foreground(cDim).Italic(true)
	sOK      = lipgloss.NewStyle().Foreground(cOK)
	sErr     = lipgloss.NewStyle().Foreground(cErr).Bold(true)
)

// Printer writes styled output to w.
type Printer struct {
	w   io.Writer
	tty bool

	mu      sync.Mutex
	stopCh  chan struct{}
	stopped chan struct{}
}

// New builds a Printer. Color/spinner are enabled only when w is an interactive
// terminal (detected via stdout); otherwise output is plain.
func New(w io.Writer) *Printer {
	tty := false
	if f, ok := w.(*os.File); ok {
		tty = isatty.IsTerminal(f.Fd())
	}
	return &Printer{w: w, tty: tty}
}

func (p *Printer) line(s string) { fmt.Fprintln(p.w, s) }

// Banner prints the branded session header.
func (p *Printer) Banner(subtitle string) {
	mark := sBrand.Render("⬡ Sahayak")
	if subtitle != "" {
		p.line(mark + sDim.Render("  "+subtitle))
	} else {
		p.line(mark)
	}
}

// Thought prints the model's reasoning line (dim, italic).
func (p *Printer) Thought(s string) {
	p.line(sThought.Render("  ◇ " + s))
}

// Note prints a Sahayak action note, e.g. an auto-repair.
func (p *Printer) Note(s string) {
	p.line(sDim.Render("    ↳ " + s))
}

// Info prints a plain informational line.
func (p *Printer) Info(s string) { p.line(s) }

// Finding prints a notable result (e.g. a diagnosis root cause), accented.
func (p *Printer) Finding(s string) {
	p.line("  " + lipgloss.NewStyle().Foreground(cWarn).Render("●") + " " + s)
}

// StepHeader prints a numbered step with the command highlighted.
func (p *Printer) StepHeader(n, total int, command string) {
	p.line("")
	p.line(sDim.Render(fmt.Sprintf("  step %d/%d", n, total)) + "  " + sCmd.Render("$ "+command))
}

// Reason prints the explanation for the current step.
func (p *Printer) Reason(why string) {
	if strings.TrimSpace(why) == "" {
		return
	}
	p.line(sDim.Render("    why  ") + why)
}

// Risk prints a colored risk badge line; auto=true marks an auto-run read-only step.
func (p *Printer) Risk(label, marker string, tier int, auto bool) {
	style := lipgloss.NewStyle().Foreground(tierColor(tier)).Bold(true)
	line := sDim.Render("    risk ") + style.Render(label+" "+marker)
	if auto {
		line += sDim.Render("  → auto-running")
	}
	p.line(line)
}

// Success / Failure print the outcome line.
func (p *Printer) Success(ms int64) {
	p.line("    " + sOK.Render("✓") + sDim.Render(fmt.Sprintf(" exit 0 · %dms", ms)))
}
func (p *Printer) Failure(code int, ms int64) {
	p.line("    " + sErr.Render("✗") + sDim.Render(fmt.Sprintf(" exit %d · %dms", code, ms)))
}
func (p *Printer) StartError(err string) {
	p.line("    " + sErr.Render("✗ could not start: ") + err)
}

// Output prints indented command output (or a computed digest).
func (p *Printer) Output(s string) {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return
	}
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		b.WriteString(sDim.Render("    │ ") + l + "\n")
	}
	fmt.Fprint(p.w, b.String())
}

// Conclusion prints the final answer in a bordered box.
func (p *Printer) Conclusion(s string) {
	title := sBrand.Render("Conclusion")
	body := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBrand).
		Padding(0, 2).
		Width(74).
		Render(strings.TrimSpace(s))
	p.line("")
	p.line(title)
	p.line(body)
}

// Spin starts an animated spinner with label and returns a stop function. On a
// non-TTY it just prints the label once and returns a no-op.
func (p *Printer) Spin(label string) func() {
	if !p.tty {
		p.line(sBrand.Render("⬡") + " " + sDim.Render(label+"…"))
		return func() {}
	}
	p.mu.Lock()
	p.stopCh = make(chan struct{})
	p.stopped = make(chan struct{})
	stop, done := p.stopCh, p.stopped
	p.mu.Unlock()

	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-stop:
				fmt.Fprint(p.w, "\r\033[K") // clear the spinner line
				close(done)
				return
			case <-ticker.C:
				fmt.Fprintf(p.w, "\r%s %s", sBrand.Render(frames[i%len(frames)]), sDim.Render(label))
				i++
			}
		}
	}()
	return func() {
		p.mu.Lock()
		ch, d := p.stopCh, p.stopped
		p.stopCh = nil
		p.mu.Unlock()
		if ch != nil {
			close(ch)
			<-d
		}
	}
}

// Streaming starts a live indicator that animates a spinner and shows a running
// token count while the model generates. Returns (onToken, stop): call onToken for
// each streamed delta, then stop() when done. On a non-TTY it prints the label once
// and both functions are no-ops.
func (p *Printer) Streaming(label string) (func(string), func()) {
	if !p.tty {
		p.line(sBrand.Render("⬡") + " " + sDim.Render(label+"…"))
		return func(string) {}, func() {}
	}
	var tokens int64
	p.mu.Lock()
	stop := make(chan struct{})
	done := make(chan struct{})
	p.mu.Unlock()

	go func() {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-stop:
				fmt.Fprint(p.w, "\r\033[K")
				close(done)
				return
			case <-ticker.C:
				n := atomic.LoadInt64(&tokens)
				meter := ""
				if n > 0 {
					meter = sDim.Render(fmt.Sprintf("  · %d tokens", n))
				}
				fmt.Fprintf(p.w, "\r%s %s%s", sBrand.Render(frames[i%len(frames)]), sDim.Render(label), meter)
				i++
			}
		}
	}()

	onToken := func(string) { atomic.AddInt64(&tokens, 1) }
	stopFn := func() {
		close(stop)
		<-done
	}
	return onToken, stopFn
}

func tierColor(tier int) lipgloss.Color {
	switch tier {
	case 0:
		return cOK
	case 1:
		return cWarn
	default:
		return cErr
	}
}
