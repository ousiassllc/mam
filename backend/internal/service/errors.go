package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

var (
	// ErrNotFound means the addressed Project, message or asset does not exist.
	ErrNotFound = errors.New("service: not found")
	// ErrConflict means the request is valid but the target is not in the required
	// state (for example, an approval card that was already answered). HTTP 409.
	ErrConflict = errors.New("service: conflict")
	// ErrBusy means the Shot is already being generated. HTTP 409.
	ErrBusy = errors.New("service: shot is being generated")
	// ErrJudgeBusy means the Take is already being judged. HTTP 409.
	ErrJudgeBusy = errors.New("service: take is being judged")
	// ErrNotJudged means the Take has no Judge result to repair from. HTTP 409.
	ErrNotJudged = errors.New("service: take has not been judged")
	// ErrAccepted means the Take passed the Judge and the minimum scores of the Retry Policy, so there is
	// nothing to repair. HTTP 409.
	ErrAccepted = errors.New("service: take is accepted")
	// ErrBudgetExceeded means not even one Take fits the budget ceiling. HTTP 402.
	ErrBudgetExceeded = errors.New("service: budget ceiling exceeded")
	// ErrUnavailable means generation is not configured (no Model Adapter, job queue or Reasoning LLM). HTTP 503.
	ErrUnavailable = errors.New("service: generation is not available")
	// ErrLocked means the change would touch a field the user has LOCKed. HTTP 423.
	ErrLocked = errors.New("service: field is locked")
	// ErrRateLimited means an external model API answered 429 (a Model Adapter, the Reasoning LLM or the
	// audio vendors). An *AIError wrapping such an answer matches it. HTTP 429.
	ErrRateLimited = errors.New("service: external model API rate limit")
	// ErrBrandViolation means the request breaks a LOCK Brand / Safety Constraint of the Project. HTTP 403.
	ErrBrandViolation = domain.ErrBrandViolation
)

// LockedError is ErrLocked with the LOCKed parameters the change touched.
type LockedError struct{ Parameters []domain.LockParameter }

func (e *LockedError) Error() string { return fmt.Sprintf("%v: %v", ErrLocked, e.Parameters) }

// Is makes errors.Is(err, ErrLocked) hold.
func (e *LockedError) Is(target error) bool { return target == ErrLocked }

// lockedError converts the repository's LOCK error, keeping the parameters.
func lockedError(err error) error {
	var le *repository.LockedParametersError
	if errors.As(err, &le) {
		return &LockedError{Parameters: le.Parameters}
	}
	return err
}

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

// AIError means the Reasoning LLM could not give a usable answer (the request itself was fine).
// Reason is the user-facing cause. HTTP 502.
type AIError struct {
	Reason string
	Err    error
}

func (e *AIError) Error() string { return "ai request failed: " + e.Reason }
func (e *AIError) Unwrap() error { return e.Err }

// Is makes errors.Is(err, ErrRateLimited) hold when the failure was a 429 from an external model API.
func (e *AIError) Is(target error) bool { return target == ErrRateLimited && rateLimited(e.Err) }

// rateLimited reports whether err is, or wraps, a 429 answer of the Reasoning LLM, a Model Adapter
// vendor or an audio vendor.
func rateLimited(err error) bool {
	var (
		api *llm.APIError
		ap  *audiodirector.ProviderError
	)
	return modeladapter.RateLimited(err) ||
		(errors.As(err, &api) && api.Status == http.StatusTooManyRequests) ||
		(errors.As(err, &ap) && ap.Status == http.StatusTooManyRequests)
}

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}
