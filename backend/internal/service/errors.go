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
