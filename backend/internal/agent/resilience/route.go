package resilience

import "errors"

// RouteFailure joins what every target of a route (primary and alternative) failed with. A target
// skipped because its circuit breaker is open (isSkip) is a failure of the route only when every
// target was skipped (the caller reports "wait and retry"); when another target was tried and failed
// for its own reason, that reason is what the caller must see (rejected, timed out, rate limited,
// ...), so the skip stays in the message but no longer matches the error that marks it.
func RouteFailure(failures []error, isSkip func(error) bool) error {
	tried := false
	for _, err := range failures {
		tried = tried || !isSkip(err)
	}
	if !tried {
		return errors.Join(failures...)
	}
	joined := make([]error, len(failures))
	for i, err := range failures {
		joined[i] = err
		if isSkip(err) {
			joined[i] = errors.New(err.Error()) // keeps the text, not the identity
		}
	}
	return errors.Join(joined...)
}
