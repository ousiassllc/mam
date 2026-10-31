package appupdate_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// syncBuffer is a log sink the schedule goroutine and the test can share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func capturedLogger(sink *syncBuffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(sink, nil))
}

func errorLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "level=ERROR") {
			lines = append(lines, l)
		}
	}
	return lines
}

// A failed manual check (POST /settings/update/check) is in the log, once, with its kind and the newer
// version; a failed signature or checksum check (ErrorVerification) leaves the same trace.
func TestService_CheckNow_LogsAFailureOnceAtError(t *testing.T) {
	for _, kind := range []updater.ErrorKind{updater.ErrorNetwork, updater.ErrorVerification} {
		t.Run(string(kind), func(t *testing.T) {
			failed := answer{err: errors.New("boom"), status: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.2.0", LastError: "boom", ErrorKind: kind}}
			var sink syncBuffer
			svc := appupdate.New(newFake(failed), "v0.1.0", func(string) {}, capturedLogger(&sink))

			_, err := svc.CheckNow(context.Background())
			require.Error(t, err)

			lines := errorLines(sink.String())
			require.Len(t, lines, 1, sink.String())
			assert.Contains(t, lines[0], "kind="+string(kind))
			assert.Contains(t, lines[0], "version=v0.2.0")
			assert.Contains(t, lines[0], "err=boom")
		})
	}
}

// "No release" and a check that found nothing to do are not failures, and a cancelled check is not one either.
func TestService_CheckNow_DoesNotLogWhatIsNotAFailure(t *testing.T) {
	var sink syncBuffer
	noRelease := answer{status: updater.Status{CheckedAt: time.Now(), NoRelease: true}}
	svc := appupdate.New(newFake(noRelease), "v0.1.0", func(string) {}, capturedLogger(&sink))
	_, err := svc.CheckNow(context.Background())
	require.NoError(t, err)

	cancelled := answer{err: context.Canceled, status: updater.Status{CheckedAt: time.Now(), LastError: "canceled", ErrorKind: updater.ErrorNetwork}}
	svc = appupdate.New(newFake(cancelled), "v0.1.0", func(string) {}, capturedLogger(&sink))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.CheckNow(ctx)
	require.Error(t, err)

	assert.Empty(t, errorLines(sink.String()), sink.String())
}

// The schedule goes through the same Check, so a failed scheduled check is logged once too (not once by
// Check and again by the schedule), for a failure it repeats and for one it does not.
func TestStart_LogsAFailedCheckOnce(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind updater.ErrorKind
	}{
		{"repeated with backoff", errors.New("no network"), updater.ErrorNetwork},
		{"not repeated", permanent{}, updater.ErrorVerification},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := answer{err: tt.err, status: updater.Status{CheckedAt: time.Now(), LastError: "x", ErrorKind: tt.kind}}
			ok := answer{status: updater.Status{CheckedAt: time.Now()}}
			checker := newFake(failed, ok)
			var sink syncBuffer
			sc := appupdate.Schedule{FirstCheck: time.Millisecond, Interval: time.Hour, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
			stop := appupdate.New(checker, "v0.1.0", func(string) {}, capturedLogger(&sink)).Start(t.Context(), sc)

			expectCalls(t, checker, 1)
			// The fake reports the call before it answers, and stop cancels the check: a check that is cancelled is not
			// logged. So wait until the schedule has handled the failed check (it says so after Check has logged it).
			require.Eventually(t, func() bool { return strings.Contains(sink.String(), "the failed check") }, wait, time.Millisecond, "the schedule handled the failed check")
			stop() // waits for the schedule goroutine

			lines := errorLines(sink.String())
			require.Len(t, lines, 1, sink.String())
			assert.Contains(t, lines[0], "kind="+string(tt.kind))
		})
	}
}
