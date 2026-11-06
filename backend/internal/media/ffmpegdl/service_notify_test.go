package ffmpegdl_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
)

// Issue #1047: a finished install is told to the app (SetOnChange) with the copy in place and before the screen says so, so the app can look
// for ffmpeg again with no restart. A removal is told too; a failed install is not.
func TestServiceTellsTheAppWhenTheDownloadedCopyChanges(t *testing.T) {
	r := newRig(t, "9.9.9")
	s := newService(t, r)
	var seen []string // what the app finds at each call: the dirs of the copy, and the state the screen has
	s.SetOnChange(func() { seen = append(seen, strings.Join(s.Dirs(), ",")+"|"+string(s.Status().Install.State)) })
	install := func() {
		started, err := s.StartInstall()
		require.NoError(t, err)
		require.True(t, started)
		s.WaitInstall()
	}
	good := r.ins.Platform.Archives[0].SHA256
	r.ins.Platform.Archives[0].SHA256 = strings.Repeat("0", 64)
	install()
	assert.Empty(t, seen, "a failed install is not told")
	r.ins.Platform.Archives[0].SHA256 = good
	install()
	require.NoError(t, s.Remove())
	assert.Equal(t, []string{r.ins.Layout.BinDir() + "|" + string(exeinstall.Running), "|" + string(exeinstall.Idle)}, seen)
}
