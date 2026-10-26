// Package gterr holds the refusals of the generation test service (package gentest and its sub-packages), so that the layers
// above map them without importing the service: *InvalidError is a refused input, *ConflictError a refusal because of the
// state, ErrNotFound a missing batch, result, prompt, Project, asset or file.
package gterr

import (
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Errors of the service.
var (
	// ErrNotFound: no such batch, result, prompt, project, asset or file.
	ErrNotFound = errors.New("gentest: not found")
	// ErrUnavailable: the part of the app the call needs is not built (no job queue).
	ErrUnavailable = errors.New("gentest: not available")
)

// InvalidError is a refused input. Message is shown to the user.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return "gentest: invalid: " + e.Message }

// ConflictError is a refusal because of the state: a changed estimate, a batch over the limit, a result that is not finished.
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return "gentest: conflict: " + e.Message }

// Invalid returns an *InvalidError with the formatted message.
func Invalid(format string, args ...any) error {
	return &InvalidError{Message: fmt.Sprintf(format, args...)}
}

// Conflict returns a *ConflictError with the formatted message.
func Conflict(format string, args ...any) error {
	return &ConflictError{Message: fmt.Sprintf(format, args...)}
}

// NotFound maps repository.ErrNotFound to ErrNotFound and leaves any other error as it is.
func NotFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
