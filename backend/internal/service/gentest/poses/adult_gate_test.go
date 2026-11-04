package poses_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
)

// Issue #1020: an install of the suggestive series that waited for the lock (another install holds it for the whole download) looks at the age gate
// again once it has it. The waiter is a second install of the series; the holder is stopped by its own context, so that the only fetch that can follow
// the closing of the gate is the waiter's. Every step is ordered by channels: the gate is closed after the waiter's first look at it, and before the holder lets go.
func TestAnAdultInstallThatWaitedForTheLockDoesNotFetchOnceTheGateIsClosed(t *testing.T) {
	r := posestest.New(t)
	r.Open.Store(true)
	r.Install(t)

	holderCtx, stopHolder := context.WithCancel(context.Background())
	defer stopHolder()
	started, release := make(chan struct{}), make(chan struct{})
	var startOnce sync.Once
	holder := make(chan error, 1)
	go func() {
		holder <- r.Lib.InstallAdult(holderCtx, true, func(_, _ int64) {
			startOnce.Do(func() { close(started) })
			<-release
		})
	}()
	<-started // the holder has the lock, in the middle of its first download

	entered := make(chan struct{})
	var enterOnce sync.Once
	looked := func() { enterOnce.Do(func() { close(entered) }) }
	r.OnGate.Store(&looked)
	waiter := make(chan error, 1)
	go func() { waiter <- r.Lib.InstallAdult(context.Background(), true, nil) }()
	<-entered // the waiter saw the gate open and is on its way to the lock

	r.Open.Store(false)
	r.OnGate.Store(nil)
	fetched := r.Requests.Load()
	stopHolder()
	close(release)

	assert.Error(t, <-holder)
	assert.ErrorIs(t, <-waiter, poses.ErrAdultGateClosed)
	assert.Equal(t, fetched, r.Requests.Load(), "nothing is fetched for the waiter after the gate was closed")
	assert.NoFileExists(t, filepath.Join(r.AdultDir, "normal", "sexy_S001_front.webp"))
}

// A gate that is closed while the series is downloading stops the download (the next asset is not fetched) and leaves no part of it in the downloads
// directory; the series is not installed, and the install can be made again when the gate is open.
func TestTheGateClosingDuringTheDownloadStopsIt(t *testing.T) {
	r := posestest.New(t)
	r.Open.Store(true)
	r.Install(t)

	err := r.Lib.InstallAdult(context.Background(), true, func(_, _ int64) { r.Open.Store(false) })
	assert.ErrorIs(t, err, poses.ErrAdultGateClosed)
	assert.NoFileExists(t, filepath.Join(r.AdultDir, "normal", "sexy_S001_front.webp"), "the last asset was never fetched")
	assert.NoDirExists(t, filepath.Join(r.Root, poses.AdultSetID, "downloads"))
	assert.False(t, r.Lib.Hides("std_P001_front"))

	r.Open.Store(true)
	require.NoError(t, r.Lib.InstallAdult(context.Background(), true, nil))
	assert.True(t, r.Lib.Status().Adult.Installed, "installed in full once the gate is open")
}
