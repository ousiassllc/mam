package resilience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/resilience"
)

var errDown = errors.New("provider down")

func hooks(waits *[]time.Duration) resilience.Hooks {
	return resilience.Hooks{
		Unavailable: func(err error) bool { return errors.Is(err, errDown) },
		RetryAfter:  func(error) time.Duration { return 0 },
		Sleep:       func(_ context.Context, d time.Duration) error { *waits = append(*waits, d); return nil },
	}
}

func newBreaker(clock *time.Time, threshold int) *resilience.Breaker {
	return resilience.NewBreaker(threshold, 30*time.Second, func() time.Time { return *clock })
}

func TestBreaker_OpensAfterConsecutiveFailuresAndProbesOnceAfterTheOpenPeriod(t *testing.T) {
	now := time.Unix(0, 0)
	b := newBreaker(&now, 2)
	assert.True(t, b.Allow())
	b.Failure()
	assert.True(t, b.Allow(), "one failure is below the threshold")
	b.Failure()
	assert.False(t, b.Allow(), "open")
	assert.True(t, b.IsOpen())

	now = now.Add(31 * time.Second)
	assert.True(t, b.Allow(), "the first caller after the open period is the probe")
	assert.False(t, b.Allow(), "everyone else waits for the probe")
	b.Failure()
	assert.False(t, b.Allow(), "a failed probe opens it again")

	now = now.Add(31 * time.Second)
	require.True(t, b.Allow())
	b.Success()
	assert.True(t, b.Allow(), "a successful probe closes it")
	assert.False(t, b.IsOpen())
}

func TestDo_RetriesUnavailableErrorsWithExponentialBackoff(t *testing.T) {
	now := time.Unix(0, 0)
	var waits []time.Duration
	calls := 0
	out, err := resilience.Do(context.Background(), newBreaker(&now, 10),
		resilience.Policy{MaxAttempts: 4, BaseDelay: time.Second, MaxDelay: 3 * time.Second}, hooks(&waits),
		func(context.Context) (string, error) {
			calls++
			if calls < 4 {
				return "", errDown
			}
			return "ok", nil
		})
	require.NoError(t, err)
	assert.Equal(t, "ok", out)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}, waits, "doubling, capped at MaxDelay")
}

func TestDo_DoesNotRetryOtherErrorsAndReturnsErrOpenWhenTheBreakerIsOpen(t *testing.T) {
	now := time.Unix(0, 0)
	var waits []time.Duration
	other := errors.New("bad request")
	calls := 0
	_, err := resilience.Do(context.Background(), newBreaker(&now, 10), resilience.Policy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Second}, hooks(&waits),
		func(context.Context) (int, error) { calls++; return 0, other })
	assert.ErrorIs(t, err, other)
	assert.Equal(t, 1, calls)
	assert.Empty(t, waits)

	b := newBreaker(&now, 1)
	b.Failure()
	_, err = resilience.Do(context.Background(), b, resilience.Policy{MaxAttempts: 3}, hooks(&waits), func(context.Context) (int, error) { calls++; return 1, nil })
	assert.ErrorIs(t, err, resilience.ErrOpen)
	assert.Equal(t, 1, calls, "no call is made through an open breaker")
}

func TestDo_StopsRetryingOnceTheFailuresOpenTheBreaker(t *testing.T) {
	now := time.Unix(0, 0)
	var waits []time.Duration
	calls := 0
	_, err := resilience.Do(context.Background(), newBreaker(&now, 2), resilience.Policy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Second}, hooks(&waits),
		func(context.Context) (int, error) { calls++; return 0, errDown })
	assert.ErrorIs(t, err, errDown, "the last failure is returned, not ErrOpen")
	assert.Equal(t, 2, calls)
}

func TestDo_RetryableNarrowsWhichFailuresAreRepeatedButAllCountForTheBreaker(t *testing.T) {
	now := time.Unix(0, 0)
	var waits []time.Duration
	h := hooks(&waits)
	h.Retryable = func(error) bool { return false }
	b := newBreaker(&now, 1)
	calls := 0
	_, err := resilience.Do(context.Background(), b, resilience.Policy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Second}, h,
		func(context.Context) (int, error) { calls++; return 0, errDown })
	assert.ErrorIs(t, err, errDown)
	assert.Equal(t, 1, calls)
	assert.True(t, b.IsOpen(), "the failure still counted")
}

func TestDo_CancelledContextReleasesTheProbeWithoutCountingAFailure(t *testing.T) {
	now := time.Unix(0, 0)
	var waits []time.Duration
	b := newBreaker(&now, 1)
	b.Failure()
	now = now.Add(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := resilience.Do(ctx, b, resilience.Policy{MaxAttempts: 3}, hooks(&waits), func(context.Context) (int, error) {
		cancel()
		return 0, errDown
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, b.Allow(), "the probe slot was given back")
}
