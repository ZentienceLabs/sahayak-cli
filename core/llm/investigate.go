package llm

import (
	"encoding/json"
	"fmt"
)

// NextAction is the model's output for one turn of the iterative investigate loop:
// a thought, the single next command to run (or done=true with a final answer).
// Proposing ONE step at a time — informed by what was already observed — is what
// lets Sahayak discover real names and drill in, instead of guessing a whole plan.
type NextAction struct {
	// Thought is the model's brief reasoning / what it learned from observations.
	Thought string `json:"thought"`
	// Action is the next command to run; nil when Done.
	Action *Step `json:"action,omitempty"`
	// Done signals the investigation is complete.
	Done bool `json:"done"`
	// FinalAnswer is the conclusion shown to the operator when Done.
	FinalAnswer string `json:"final_answer,omitempty"`
}

// ParseNextAction extracts a NextAction from a model reply (tolerant of fences /
// prose) and normalizes the action's command.
func ParseNextAction(raw string) (NextAction, error) {
	var na NextAction
	body, err := extractJSON(raw)
	if err != nil {
		return na, err
	}
	if err := json.Unmarshal([]byte(body), &na); err != nil {
		return na, fmt.Errorf("next-action json invalid: %w", err)
	}
	if na.Action != nil {
		n := na.Action.Normalized()
		na.Action = &n
		// A blank/placeholder command means "nothing useful to do" → treat as done.
		if n.Command == "" {
			na.Action = nil
			na.Done = true
		}
	}
	return na, nil
}
