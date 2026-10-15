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

type askedWait struct{ after time.Duration }

func (e askedWait) Error() string { return "slow down" }

func repeatHooks(waits *[]time.Duration) resilience.Hooks {
	h := hooks(waits)
	h.Unavailable = func(err error) bool { return errors.Is(err, errDown) || errors.As(err, new(askedWait)) }
	h.RetryAfter = func(err error) time.Duration {
		var a askedWait
		if errors.As(err, &a) {
			return a.after
		}
		return 0
	}
	return h
}

func failTimes(n int, err error) func() error {
	calls := 0
	return func() error {
		if calls++; calls <= n {
			return err
		}
		return nil
	}
}

func TestRepeat_OutlastsAnOutageOfManyTriesWithABoundedBackoff(t *testing.T) {
	var waits []time.Duration
	bo := resilience.Backoff{Base: 5 * time.Second, Max: time.Minute}

	err := resilience.Repeat(context.Background(), bo, repeatHooks(&waits), failTimes(8, errDown))

	require.NoError(t, err)
	s := time.Second
	assert.Equal(t, []time.Duration{5 * s, 10 * s, 20 * s, 40 * s, 60 * s, 60 * s, 60 * s, 60 * s}, waits, "doubling, capped at Max")
}

func TestRepeat_WaitsForTheRetryAfterWhenItIsLongerThanTheBackoffButNotBeyondMax(t *testing.T) {
	var waits []time.Duration
	bo := resilience.Backoff{Base: time.Second, Max: time.Minute}
	calls := 0
	fn := func() error {
		calls++
		switch calls {
		case 1:
			return askedWait{after: 45 * time.Second}
		case 2:
			return askedWait{after: time.Second} // shorter than the backoff (2s)
		case 3:
			return askedWait{after: time.Hour}
		}
		return nil
	}

	require.NoError(t, resilience.Repeat(context.Background(), bo, repeatHooks(&waits), fn))

	assert.Equal(t, []time.Duration{45 * time.Second, 2 * time.Second, time.Minute}, waits)
}

func TestRepeat_ReturnsAnErrorThatIsNotUnavailableAtOnce(t *testing.T) {
	var waits []time.Duration
	other := errors.New("bad request")
	calls := 0

	err := resilience.Repeat(context.Background(), resilience.Backoff{Base: time.Second, Max: time.Second}, repeatHooks(&waits),
		func() error { calls++; return other })

	assert.ErrorIs(t, err, other)
	assert.Equal(t, 1, calls)
	assert.Empty(t, waits)
}

func TestRepeat_StopsWhenTheContextEndsAndReturnsTheLastError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var waits []time.Duration
	h := repeatHooks(&waits)
	calls := 0

	err := resilience.Repeat(ctx, resilience.Backoff{Base: time.Second, Max: time.Second}, h, func() error {
		if calls++; calls == 3 {
			cancel() // the job's deadline passes while the vendor is still down
		}
		return errDown
	})

	assert.ErrorIs(t, err, errDown)
	assert.Equal(t, 3, calls)
}

func TestRepeat_StopsWhenTheWaitIsInterrupted(t *testing.T) {
	h := resilience.Hooks{
		Unavailable: func(error) bool { return true },
		Sleep:       func(context.Context, time.Duration) error { return context.DeadlineExceeded },
	}
	calls := 0

	err := resilience.Repeat(context.Background(), resilience.Backoff{Base: time.Second}, h, func() error { calls++; return errDown })

	assert.ErrorIs(t, err, errDown)
	assert.Equal(t, 1, calls)
}

func TestRepeat_RealSleepIsCutShortByTheContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()

	err := resilience.Repeat(ctx, resilience.Backoff{Base: time.Hour, Max: time.Hour},
		resilience.Hooks{Unavailable: func(error) bool { return true }}, func() error { return errDown })

	assert.ErrorIs(t, err, errDown)
	assert.Less(t, time.Since(start), time.Minute)
}
