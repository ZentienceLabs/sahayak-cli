package exec

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// Result captures everything Sahayak needs to know about a finished command:
// what ran, how it ended, and what it produced. The diagnosis engine consumes
// this (after redaction) when ExitCode != 0.
type Result struct {
	Command    string
	Args       []string
	ExitCode   int
	Stdout     string
	Stderr     string
	DurationMS int64
	// Err is set for failures to even start the process (binary not found, etc.),
	// distinct from a process that ran and returned non-zero.
	Err error
}

// Success reports whether the command ran and exited 0.
func (r Result) Success() bool { return r.Err == nil && r.ExitCode == 0 }

// Runner executes commands. It deliberately uses os/exec with an explicit arg
// slice and NO intermediate shell (`sh -c`), eliminating shell-injection surface:
// what you approve in the gate is exactly what runs.
type Runner struct {
	// Dir is the working directory; empty means the current process dir.
	Dir string
	// Env, when non-nil, replaces the child environment; nil inherits the parent.
	Env []string
}

// NewRunner returns a Runner using the current working directory and environment.
func NewRunner() *Runner { return &Runner{} }

// Run executes command+args, capturing stdout/stderr and the exit code. It never
// routes through a shell. ctx cancellation kills the process.
func (r *Runner) Run(ctx context.Context, command string, args []string) Result {
	start := time.Now()
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = r.Dir
	cmd.Env = r.Env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	res := Result{
		Command:    command,
		Args:       args,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: time.Since(start).Milliseconds(),
	}

	if runErr == nil {
		res.ExitCode = 0
		return res
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		// The process ran and returned non-zero — a normal failure we diagnose.
		res.ExitCode = exitErr.ExitCode()
		return res
	}

	// Failed to start (e.g. binary not found): surface as an error with code -1.
	res.ExitCode = -1
	res.Err = runErr
	return res
}
