package credential

import (
	"errors"
	"fmt"
	"time"
)

// DefaultTimeout is how long a call to the OS secure storage may take before it is given up. The OS
// store answers in milliseconds; it hangs when the Linux Secret Service waits for an unlock prompt
// nobody can answer (go-keyring cannot cancel that call).
const DefaultTimeout = 5 * time.Second

// ErrUnavailable is returned when the OS secure storage did not answer within the timeout. Callers treat
// it like any other store failure (the settings screen loses only its API key panel).
var ErrUnavailable = errors.New("credential: OS secure storage did not respond in time")

// WithTimeout returns a Store that gives up on a call of inner after timeout with ErrUnavailable.
// The abandoned call keeps running in its goroutine and its result is dropped.
func WithTimeout(inner Store, timeout time.Duration) Store {
	return &timed{inner: inner, timeout: timeout}
}

type timed struct {
	inner   Store
	timeout time.Duration
}

// within runs fn and returns its result, or ErrUnavailable (wrapped with op) when it takes longer than timeout.
func within[T any](timeout time.Duration, op string, fn func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1) // buffered: an abandoned call must not block forever on the send
	go func() {
		v, err := fn()
		done <- result{v, err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.value, r.err
	case <-timer.C:
		var zero T
		return zero, fmt.Errorf("%s: %w", op, ErrUnavailable)
	}
}

// Set implements Store.
func (t *timed) Set(provider, apiKey string) error {
	_, err := within(t.timeout, fmt.Sprintf("store api key for %q", provider), func() (struct{}, error) {
		return struct{}{}, t.inner.Set(provider, apiKey)
	})
	return err
}

// Get implements Store.
func (t *timed) Get(provider string) (string, error) {
	return within(t.timeout, fmt.Sprintf("load api key for %q", provider), func() (string, error) {
		return t.inner.Get(provider)
	})
}

// Delete implements Store.
func (t *timed) Delete(provider string) error {
	_, err := within(t.timeout, fmt.Sprintf("delete api key for %q", provider), func() (struct{}, error) {
		return struct{}{}, t.inner.Delete(provider)
	})
	return err
}

// Has implements Store.
func (t *timed) Has(provider string) (bool, error) {
	return within(t.timeout, fmt.Sprintf("check api key for %q", provider), func() (bool, error) {
		return t.inner.Has(provider)
	})
}
