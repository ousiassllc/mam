package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// cuttingClient answers like inner, but while cut is set the body ends after a few bytes with a cut-off connection.
type cuttingClient struct {
	inner httpclient.Doer
	cut   *atomic.Bool
}

func (c cuttingClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.inner.Do(req)
	if err != nil || !c.cut.Load() {
		return resp, err
	}
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(io.LimitReader(resp.Body, 10), errReader{io.ErrUnexpectedEOF}), resp.Body}
	return resp, nil
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

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

// A download of the engine whose connection keeps breaking is resumed by itself, each resume is logged (Issue #1056), and when
// every transfer was cut the user reads that the connection was lost and that installing again continues from the part file.
func TestInstallLogsEachResumeAndTellsAConnectionLostInJapanese(t *testing.T) {
	logs := &logBuf{}
	var cut atomic.Bool
	cut.Store(true)
	ir := newInstallRig(t, func(o *codex.Options) {
		o.Logger = slog.New(slog.NewJSONHandler(logs, nil))
		o.Fetch.Client = cuttingClient{inner: o.Fetch.Client, cut: &cut}
		o.Fetch.Retry = fetch.Retry{Attempts: 3, Backoff: 1, MaxBackoff: 1}
	})

	ir.install(t)

	st := ir.svc.Status()
	if st.Install.State != exeinstall.Failed {
		t.Fatalf("status = %+v", st.Install)
	}
	if st.Install.Error != "通信が切れました。もう一度導入すると続きから再開します" {
		t.Errorf("Error = %q", st.Install.Error)
	}
	if strings.Contains(st.Install.Error, "unexpected EOF") {
		t.Errorf("the English error of the connection reached the user: %q", st.Install.Error)
	}
	resumes := logs.records("WARN", "download interrupted, resuming from the part file")
	if len(resumes) != 2 {
		t.Fatalf("resumes logged = %d, want 2 (three transfers)", len(resumes))
	}
	for i, rec := range resumes {
		if rec["target"] != "codex" || rec["attempt"] != float64(i+1) || rec["max_attempts"] != float64(3) || rec["err"] == "" {
			t.Errorf("resume %d = %v", i, rec)
		}
	}

	cut.Store(false) // the network is back: installing again continues and succeeds
	ir.install(t)
	if st := ir.svc.Status(); st.Install.State != exeinstall.Succeeded {
		t.Fatalf("after the retry: %+v", st.Install)
	}
}
