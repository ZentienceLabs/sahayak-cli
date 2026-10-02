package tui

import (
	"os"

	"github.com/mattn/go-isatty"
)

// IsInteractive reports whether both stdin and stdout are attached to a terminal.
// The rich approval gate requires a real TTY; over SSH pipes, in CI, or under cron
// the caller falls back to the line-mode gate so automation never hangs on the UI.
func IsInteractive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}
