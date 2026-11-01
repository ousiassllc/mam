package ffmpegdl_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

func TestTheDefaultCheckRunsTheExecutableWithVersion(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("the fake executables are shell scripts")
	}
	r := newRig(t, "9.9.9")
	r.ins.Check = nil
	require.NoError(t, r.ins.Install(context.Background(), nil), "scripts that answer -version like ffmpeg does")

	bad := newRig(t, "9.9.9")
	bad.ins.Check = nil
	notFFmpeg := "#!/bin/sh\necho 'something else'\n"
	body := zipOf(t, [2]string{"build/bin/ffmpeg", notFFmpeg}, [2]string{"build/bin/ffprobe", fakeFFprobe})
	bad.bodies["/dl/9.9.9/ffmpeg.zip"] = body
	a := &bad.ins.Platform.Archives[0]
	a.SHA256, a.Size = sum(string(body)), int64(len(body))
	a.Members[0].Size, a.Members[0].SHA256 = int64(len(notFFmpeg)), sum(notFFmpeg)
	err := bad.ins.Install(context.Background(), nil)
	require.ErrorIs(t, err, ffmpegdl.ErrNotRunnable, "an answer that is not ffmpeg's is not an ffmpeg")
	bad.nothingInstalled()
}

func TestCancelKeepsThePartFileAndAnotherInstallResumes(t *testing.T) {
	r := newRig(t, "9.9.9")
	body := r.bodies["/dl/9.9.9/ffmpeg.zip"]
	ctx, cancel := context.WithCancel(context.Background())
	var ranges []string
	var mu sync.Mutex
	r.onServe = func(w http.ResponseWriter, req *http.Request) bool {
		mu.Lock()
		ranges = append(ranges, req.Header.Get("Range"))
		first := len(ranges) == 1
		mu.Unlock()
		if !first {
			return false
		}
		// The first request delivers half of the archive and then the user presses cancel.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:len(body)/2])
		w.(http.Flusher).Flush()
		<-req.Context().Done()
		return true
	}
	// The user presses cancel once a byte is on disk.
	err := r.ins.Install(ctx, func(_ ffmpegdl.Phase, done, _ int64) {
		if done > 0 {
			cancel()
		}
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, r.ins.Status().Installed)
	part, statErr := os.Stat(fetch.PartPath(r.archivePath()))
	require.NoError(t, statErr, "the part file stays")
	assert.Positive(t, part.Size())

	require.NoError(t, r.ins.Install(context.Background(), nil))
	assert.True(t, r.ins.Status().Installed)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, ranges, 2)
	assert.True(t, strings.HasPrefix(ranges[1], "bytes="), "the second install continues the download: %q", ranges[1])
}

func TestInstallRefusesWhenThereIsNoRoomBeforeDownloadingAnything(t *testing.T) {
	r := newRig(t, "9.9.9")
	r.ins.FreeBytes = func(string) (uint64, error) { return 10, nil }
	var requests int
	r.onServe = func(http.ResponseWriter, *http.Request) bool { requests++; return false }
	var noSpace *fetch.NoSpaceError
	require.ErrorAs(t, r.ins.Install(context.Background(), nil), &noSpace)
	assert.Zero(t, requests)
	assert.Equal(t, uint64(10), noSpace.Free)
	assert.Equal(t, r.ins.NeedBytes(), int64(noSpace.Need))
}

func TestInstallRefusesAnUnwritableDirectory(t *testing.T) {
	r := newRig(t, "9.9.9")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	r.ins.Layout = ffmpegdl.Layout{Root: filepath.Join(file, "ffmpeg")}
	require.ErrorIs(t, r.ins.Install(context.Background(), nil), exeinstall.ErrNotWritable)
}
