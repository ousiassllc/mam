package testenv_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/testenv"
)

func TestMissingFFmpeg_SkipsOutsideCI(t *testing.T) {
	t.Setenv("CI", "")
	skipped := true
	t.Run("inner", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		testenv.MissingFFmpeg(t, "ffmpeg is not installed")
	})
	assert.True(t, skipped)
}

func TestMissingFFmpeg_FailsInCI(t *testing.T) {
	t.Setenv("CI", "true")
	var failed bool
	fake := &recorder{TB: t}
	func() {
		defer func() { _ = recover() }()
		testenv.MissingFFmpeg(fake, "ffmpeg is not installed")
	}()
	failed = fake.fatal
	assert.True(t, failed)
}

// recorder records Fatalf instead of failing the real test, and stops like Fatalf does.
type recorder struct {
	testing.TB
	fatal bool
}

func (r *recorder) Fatalf(string, ...any) {
	r.fatal = true
	panic("fatal")
}
func (r *recorder) Skip(...any) { panic("skip") }

func TestParallel_MayBeCalledTwiceOnTheSameTest(t *testing.T) {
	testenv.Parallel(t)
	assert.NotPanics(t, func() { testenv.Parallel(t) })
}

func TestEndsAfterFirstRequest_StartsTheGraceWithTheFirstRequest(t *testing.T) {
	var requests atomic.Int32
	ctx := testenv.EndsAfterFirstRequest(t, &requests, 20*time.Millisecond)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, ctx.Err(), "no request yet: the call must not end")

	requests.Add(1)
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not end after the first request")
	}
}
