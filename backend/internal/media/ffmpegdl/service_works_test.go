package ffmpegdl_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// The install is recorded for the job list (worktrack): running while it downloads, then its end, one record per install. A cancel is
// not a failure and a failure carries the words the settings screen shows.
func TestServiceRecordsEachInstallForTheJobList(t *testing.T) {
	r := newRig(t, "9.9.9")
	release, reached := make(chan struct{}), make(chan struct{}, 1)
	body := r.bodies["/dl/9.9.9/ffmpeg.zip"]
	var attempt int
	r.onServe = func(w http.ResponseWriter, req *http.Request) bool {
		attempt++
		switch attempt {
		case 1: // a download that is stopped half-way
			w.Header().Set("Content-Length", "100000000")
			_, _ = w.Write(body[:10])
			w.(http.Flusher).Flush()
			reached <- struct{}{}
			<-req.Context().Done()
			return true
		case 2: // held until the test has looked at the running install
			reached <- struct{}{}
			<-release
		}
		return false
	}
	s := newService(t, r)
	assert.Empty(t, s.Works(), "nothing was started")

	started, err := s.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	<-reached
	s.CancelInstall()
	s.WaitInstall()
	works := s.Works()
	require.Len(t, works, 1)
	assert.Equal(t, worktrack.KindFFmpegInstall, works[0].Kind)
	assert.Equal(t, worktrack.Cancelled, works[0].State)
	assert.Empty(t, works[0].Error, "pressing cancel is not an error")

	started, err = s.StartInstall()
	require.NoError(t, err)
	require.True(t, started)
	<-reached
	works = s.Works()
	require.Len(t, works, 2, "a new install is a new record")
	assert.Equal(t, worktrack.Running, works[0].State)
	assert.NotEmpty(t, works[0].Text, "what it is doing")
	assert.Nil(t, works[0].FinishedAt)
	close(release)
	s.WaitInstall()
	works = s.Works()
	assert.Equal(t, worktrack.Succeeded, works[0].State)
	assert.NotNil(t, works[0].FinishedAt)

	bad := newRig(t, "9.9.9")
	bad.ins.Platform.Archives[0].SHA256 = strings.Repeat("0", 64)
	failing := newService(t, bad)
	_, err = failing.StartInstall()
	require.NoError(t, err)
	failing.WaitInstall()
	require.Len(t, failing.Works(), 1)
	assert.Equal(t, worktrack.Failed, failing.Works()[0].State)
	assert.Contains(t, failing.Works()[0].Error, "SHA-256")
}
