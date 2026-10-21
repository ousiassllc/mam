package appupdate_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// fakeChecker answers the checks in order (the last answer repeats) and tells each call on called.
type fakeChecker struct {
	mu      sync.Mutex
	answers []answer
	status  updater.Status
	calls   int
	called  chan struct{}
	block   chan struct{} // when set, a check waits for it to be closed
}

type answer struct {
	result updater.Result
	err    error
	status updater.Status
}

func (f *fakeChecker) CheckForUpdate(ctx context.Context) (updater.Result, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return updater.Result{}, ctx.Err()
		}
	}
	f.mu.Lock()
	a := f.answers[min(f.calls, len(f.answers)-1)]
	f.calls++
	f.status = a.status
	f.mu.Unlock()
	if f.called != nil {
		f.called <- struct{}{}
	}
	return a.result, a.err
}

func (f *fakeChecker) Status() updater.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func newFake(answers ...answer) *fakeChecker {
	return &fakeChecker{answers: answers, called: make(chan struct{}, 100)}
}

func TestService_CheckNow_HandsAVerifiedInstallerOverOnce(t *testing.T) {
	ready := answer{
		result: updater.Result{Ready: true, Version: "v0.2.0", InstallerPath: "/tmp/mam-update-1/setup.exe"},
		status: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.2.0", Ready: true},
	}
	checker := newFake(ready)
	var installed []string
	svc := appupdate.New(checker, "v0.1.0", func(p string) { installed = append(installed, p) }, nil)

	v, err := svc.CheckNow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, appupdate.StateReady, v.State)
	assert.Equal(t, "v0.2.0", v.Latest)
	assert.Equal(t, []string{"/tmp/mam-update-1/setup.exe"}, installed)

	_, err = svc.CheckNow(context.Background())
	require.NoError(t, err)
	assert.Len(t, installed, 1, "no second installer is handed over while the app quits")
	assert.Equal(t, 1, checker.calls, "no further check once the installer was handed over")
}

func TestService_CheckNow_NotReadyInstallsNothing(t *testing.T) {
	blocked := answer{status: updater.Status{CheckedAt: time.Now(), Available: true, Version: "v0.2.0", Blocked: true, BlockedKind: updater.BlockJobs}}
	svc := appupdate.New(newFake(blocked), "v0.1.0", func(string) { t.Fatal("install called without a verified installer") }, nil)

	v, err := svc.CheckNow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, appupdate.StateBlocked, v.State)
	assert.Equal(t, appupdate.BlockJobs, v.BlockedBy)
}

func TestService_CheckNow_ReturnsTheFailureAndShowsItsKind(t *testing.T) {
	failed := answer{err: errors.New("boom"), status: updater.Status{CheckedAt: time.Now(), LastError: "boom", ErrorKind: updater.ErrorNetwork}}
	svc := appupdate.New(newFake(failed), "v0.1.0", func(string) {}, nil)

	v, err := svc.CheckNow(context.Background())
	require.Error(t, err)
	assert.Equal(t, appupdate.StateError, v.State)
	assert.Equal(t, appupdate.ErrorNetwork, v.ErrorKind)
}

func TestService_View_StateOfEveryStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name   string
		status updater.Status
		want   appupdate.State
	}{
		{"nothing checked yet", updater.Status{}, appupdate.StateUnchecked},
		{"dev build", updater.Status{CheckedAt: now, DevBuild: true}, appupdate.StateDevBuild},
		{"no release published", updater.Status{CheckedAt: now, NoRelease: true}, appupdate.StateNoRelease},
		{"up to date", updater.Status{CheckedAt: now}, appupdate.StateUpToDate},
		{"newer release being fetched", updater.Status{CheckedAt: now, Available: true, Version: "v0.2.0"}, appupdate.StateAvailable},
		{"error wins over a seen release", updater.Status{CheckedAt: now, Available: true, Version: "v0.2.0", Blocked: true, LastError: "x", ErrorKind: updater.ErrorAccess}, appupdate.StateError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := appupdate.New(newFake(answer{status: tt.status}), "v0.1.0", func(string) {}, nil)
			_, _ = svc.CheckNow(context.Background())
			v := svc.View()
			assert.Equal(t, tt.want, v.State)
			assert.Equal(t, "v0.1.0", v.Version)
		})
	}
}

func TestService_View_ShowsACheckThatRuns(t *testing.T) {
	checker := newFake(answer{status: updater.Status{CheckedAt: time.Now()}})
	checker.block = make(chan struct{})
	svc := appupdate.New(checker, "v0.1.0", func(string) {}, nil)
	done := make(chan struct{})
	go func() { _, _ = svc.CheckNow(context.Background()); close(done) }()

	require.Eventually(t, func() bool { return svc.View().State == appupdate.StateChecking }, 5*time.Second, time.Millisecond)
	close(checker.block)
	<-done
	assert.Equal(t, appupdate.StateUpToDate, svc.View().State)
}
