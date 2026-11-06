package bgwork_test

import (
	"bytes"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/bgwork"
)

func TestGroup_WaitReturnsOnlyAfterEveryStartedFunctionEnded(t *testing.T) {
	var g bgwork.Group
	var done atomic.Int32
	release := make(chan struct{})
	for range 3 {
		g.Go(slog.New(slog.DiscardHandler), "some work", func() {
			<-release
			done.Add(1)
		})
	}
	close(release)
	g.Wait()
	assert.Equal(t, int32(3), done.Load())
}

func TestGroup_APanickingFunctionIsLoggedAndWaitStillReturns(t *testing.T) {
	var g bgwork.Group
	var buf bytes.Buffer
	g.Go(slog.New(slog.NewTextHandler(&buf, nil)), "some work", func() { panic("boom") })
	g.Wait()
	assert.Contains(t, buf.String(), "some work panicked")
}
