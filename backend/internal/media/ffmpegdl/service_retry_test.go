package ffmpegdl_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media/ffmpegdl"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// logBuf is the JSON log of the Service, safe to read while it is written.
type logBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// records are the log lines of the level with the message.
func (l *logBuf) records(level, msg string) []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(l.buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if json.Unmarshal(line, &rec) == nil && rec["level"] == level && rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

// A download whose connection keeps breaking is resumed by itself and each resume is logged (Issue #1056). When every transfer was
// cut the user reads that the connection was lost and that installing again continues from the part file, and it does.
func TestServiceLogsEachResumeAndTellsAConnectionLostInJapanese(t *testing.T) {
	r := newRig(t, "9.9.9")
	body := r.bodies["/dl/9.9.9/ffmpeg.zip"]
	var cut atomic.Bool
	cut.Store(true)
	r.onServe = func(w http.ResponseWriter, _ *http.Request) bool {
		if !cut.Load() {
			return false
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:10])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // the connection breaks in the middle of the body
	}
	logs := &logBuf{}
	s := ffmpegdl.New(ffmpegdl.Options{
		Layout:  r.ins.Layout,
		Catalog: ffmpegdl.Catalog{Platforms: map[string]ffmpegdl.Platform{ffmpegdl.Key("linux", "amd64"): r.ins.Platform}},
		GOOS:    "linux", GOARCH: "amd64",
		Fetch:  fetch.Options{Client: r.srv.Client(), CheckURL: func(*url.URL) error { return nil }, Retry: fetch.Retry{Attempts: 3, Backoff: 1, MaxBackoff: 1}},
		Check:  r.ins.Check,
		Logger: slog.New(slog.NewJSONHandler(logs, nil)),
	})
	t.Cleanup(s.Close)

	install := func() {
		started, err := s.StartInstall()
		require.NoError(t, err)
		require.True(t, started)
		s.WaitInstall()
	}
	install()

	st := s.Status()
	assert.Equal(t, exeinstall.Failed, st.Install.State)
	assert.Equal(t, "通信が切れました。もう一度導入すると続きから再開します", st.Install.Error)
	resumes := logs.records("WARN", "download interrupted, resuming from the part file")
	require.Len(t, resumes, 2, "three transfers: two resumes")
	for i, rec := range resumes {
		assert.Equal(t, "ffmpeg", rec["target"])
		assert.EqualValues(t, i+1, rec["attempt"])
		assert.EqualValues(t, 3, rec["max_attempts"])
		assert.NotEmpty(t, rec["err"])
	}

	cut.Store(false) // the network is back
	install()
	assert.Equal(t, exeinstall.Succeeded, s.Status().Install.State, s.Status().Install.Error)
}
