// Package tui provides Sahayak's rich terminal UI built on Bubble Tea. Today it
// supplies the interactive approval gate (an Approver implementation); later
// phases add streaming plan views and diagnosis panels. The TUI is only used on an
// interactive terminal — see IsInteractive — with the line-mode gate as fallback.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ZentienceLabs/sahayak-cli/core/exec"
)

// Brand and semantic colors. Kept in one place so later TUI surfaces stay consistent.
var (
	colWhisper  = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}
	colCommand  = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#22D3EE"}
	colReadOnly = lipgloss.Color("#22C55E")
	colMutating = lipgloss.Color("#EAB308")
	colDestroy  = lipgloss.Color("#EF4444")

	styleTitle   = lipgloss.NewStyle().Bold(true)
	styleWhisper = lipgloss.NewStyle().Foreground(colWhisper)
	styleCommand = lipgloss.NewStyle().Foreground(colCommand).Bold(true)
	styleKeyHint = lipgloss.NewStyle().Foreground(colWhisper)
)

// riskColor maps a risk tier to its accent color (used for the card border + badge).
func riskColor(r exec.Risk) lipgloss.Color {
	switch r {
	case exec.ReadOnly:
		return colReadOnly
	case exec.Mutating:
		return colMutating
	case exec.Destructive:
		return colDestroy
	default:
		return lipgloss.Color("#9CA3AF")
	}
}

// riskBadge renders a colored, human-readable risk label.
func riskBadge(r exec.Risk) string {
	style := lipgloss.NewStyle().Foreground(riskColor(r)).Bold(true)
	switch r {
	case exec.ReadOnly:
		return style.Render("read-only ✓")
	case exec.Mutating:
		return style.Render("mutating ⚠")
	case exec.Destructive:
		return style.Render("destructive ‼ DANGER")
	default:
		return style.Render("unknown ?")
	}
}
