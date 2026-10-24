package appupdate_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

const wait = 5 * time.Second

var fast = appupdate.Schedule{FirstCheck: time.Millisecond, Interval: 20 * time.Millisecond, InitialBackoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond}

// expectCalls waits for n checks, failing when they do not come.
func expectCalls(t *testing.T, c *fakeChecker, n int) {
	t.Helper()
	for range n {
		select {
		case <-c.called:
		case <-time.After(wait):
			t.Fatal("the expected check did not run")
		}
	}
}

func TestStart_ChecksAtStartupThenEveryInterval(t *testing.T) {
	ok := answer{status: updater.Status{CheckedAt: time.Now()}}
	checker := newFake(ok)
	svc := appupdate.New(checker, "v0.1.0", func(string) {}, nil)
	stop := svc.Start(t.Context(), fast)
	defer stop()

	expectCalls(t, checker, 3) // the first one and two scheduled ones
}

func TestStart_RepeatsATransientFailureAndAHeldBackReleaseWithBackoff(t *testing.T) {
	failed := answer{err: errors.New("no network"), status: updater.Status{CheckedAt: time.Now(), LastError: "no network", ErrorKind: updater.ErrorNetwork}}
	held := answer{status: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.2.0", Blocked: true, BlockedKind: updater.BlockMedia}}
	ready := answer{
		result: updater.Result{Ready: true, Version: "v0.2.0", InstallerPath: "/tmp/setup.exe"},
		status: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.2.0", Ready: true},
	}
	checker := newFake(failed, failed, held, held, ready)
	installed := make(chan string, 1)
	// The interval is far longer than the test: only the backoff can repeat the checks.
	sc := appupdate.Schedule{FirstCheck: time.Millisecond, Interval: time.Hour, InitialBackoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond}
	stop := appupdate.New(checker, "v0.1.0", func(p string) { installed <- p }, nil).Start(t.Context(), sc)
	defer stop()

	select {
	case p := <-installed:
		assert.Equal(t, "/tmp/setup.exe", p)
	case <-time.After(wait):
		t.Fatal("the release was not installed after the retries")
	}
	expectCalls(t, checker, 5)
}

// A failure that only another release can fix (a bad signature, a removed asset) waits for the next
// scheduled check instead of downloading the installer again every backoff step.
func TestStart_DoesNotRetryAPermanentFailureWithBackoff(t *testing.T) {
	checker := newFake(answer{err: permanent{}, status: updater.Status{CheckedAt: time.Now(), LastError: "bad", ErrorKind: updater.ErrorVerification}})
	sc := appupdate.Schedule{FirstCheck: time.Millisecond, Interval: time.Hour, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
	stop := appupdate.New(checker, "v0.1.0", func(string) {}, nil).Start(t.Context(), sc)
	defer stop()

	expectCalls(t, checker, 1)
	select {
	case <-checker.called:
		t.Fatal("a permanent failure was retried before the interval")
	case <-time.After(50 * time.Millisecond): // a negative check: nothing may come in this window
	}
}

func TestStart_ADevBuildChecksOnceAndStops(t *testing.T) {
	checker := newFake(answer{status: updater.Status{CheckedAt: time.Now(), DevBuild: true}})
	stop := appupdate.New(checker, "dev", func(string) {}, nil).Start(t.Context(), fast)
	defer stop()

	expectCalls(t, checker, 1)
	select {
	case <-checker.called:
		t.Fatal("a dev build kept checking")
	case <-time.After(100 * time.Millisecond): // a negative check: nothing may come in this window
	}
}

func TestStart_StopEndsTheScheduleAndCancelsARunningCheck(t *testing.T) {
	checker := newFake(answer{status: updater.Status{CheckedAt: time.Now()}})
	checker.block = make(chan struct{}) // never closed: the check only ends by cancellation
	stop := appupdate.New(checker, "v0.1.0", func(string) {}, nil).Start(t.Context(), fast)

	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(wait):
		t.Fatal("stop did not return")
	}
	require.Zero(t, checker.calls)
}

type permanent struct{}

func (permanent) Error() string   { return "permanent" }
func (permanent) Permanent() bool { return true }
