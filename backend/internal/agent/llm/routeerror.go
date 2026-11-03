package llm

import (
	"errors"
	"fmt"
)

// TargetFailure is one target a call tried and how it failed.
type TargetFailure struct {
	Target Target
	Err    error
}

// RouteError is the failure of a call whose every target (the primary and the alternative of the Agent's route) failed. It
// reads like the joined failures of the targets (errors.Is / errors.As see through it), and also says which Agent was called,
// which targets were tried and whether a saved setting is still waiting for a restart: what the user needs to fix a failure
// that the log alone explains.
type RouteError struct {
	Agent Agent
	// Tried are the targets in the order they were called, with the failure of each (a target skipped by its circuit breaker
	// is in it with ErrCircuitOpen).
	Tried []TargetFailure
	// RestartPending says which saved settings the running client has not taken (Client.Apply); nil for none.
	RestartPending []string
	err            error
}

func (e *RouteError) Error() string {
	return fmt.Sprintf("llm: every provider failed for agent %s: %v", e.Agent, e.Unwrap())
}

// Unwrap returns the failures of the targets, joined. A RouteError made by hand (a test double of the client) has only Tried, which
// are joined.
func (e *RouteError) Unwrap() error {
	if e.err != nil {
		return e.err
	}
	errs := make([]error, len(e.Tried))
	for i, f := range e.Tried {
		errs[i] = fmt.Errorf("%s/%s: %w", f.Target.Provider, f.Target.Model, f.Err)
	}
	return errors.Join(errs...)
}
