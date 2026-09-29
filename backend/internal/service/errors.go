package service

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound means the addressed Project, message or asset does not exist.
	ErrNotFound = errors.New("service: not found")
	// ErrConflict means the request is valid but the target is not in the required
	// state (for example, an approval card that was already answered). HTTP 409.
	ErrConflict = errors.New("service: conflict")
	// ErrBusy means the Shot is already being generated. HTTP 409.
	ErrBusy = errors.New("service: shot is being generated")
	// ErrBudgetExceeded means not even one Take fits the budget ceiling. HTTP 402.
	ErrBudgetExceeded = errors.New("service: budget ceiling exceeded")
	// ErrUnavailable means generation is not configured (no Model Adapter, job queue or Reasoning LLM). HTTP 503.
	ErrUnavailable = errors.New("service: generation is not available")
	// ErrLocked means the change would touch a field the user has LOCKed. HTTP 423.
	ErrLocked = errors.New("service: field is locked")
)

// ValidationError is invalid user input. Field names the form field ("" for the
// request as a whole); Message is shown to the user. HTTP 422.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return "invalid input: " + e.Message
	}
	return fmt.Sprintf("invalid input: %s: %s", e.Field, e.Message)
}

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
