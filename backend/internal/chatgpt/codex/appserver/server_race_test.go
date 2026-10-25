package appserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// gatedProc is a fake process whose Stop waits for the test, once the gate is armed.
type gatedProc struct {
	*codextest.Process
	entered chan struct{} // closed when Stop was called
	release chan struct{} // closed by the test
	once    sync.Once
}

func newGatedProc(p *codextest.Process) *gatedProc {
	return &gatedProc{Process: p, entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *gatedProc) Stop() {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	g.Process.Stop()
}

// signalHandler closes ch at the first record with the message.
type signalHandler struct {
	slog.Handler
	msg  string
	ch   chan struct{}
	once *sync.Once
}

func (h signalHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message == h.msg {
		h.once.Do(func() { close(h.ch) })
	}
	return nil
}

func (h signalHandler) Enabled(context.Context, slog.Level) bool { return true }

func TestTheEndOfAnOldProcessDoesNotMarkANewerHealthySessionFailed(t *testing.T) {
	fake := codextest.New(t, home)
	var first *gatedProc
	var mu sync.Mutex
	ended := make(chan struct{})
	srv := appserver.New(appserver.Options{
		CodexHome: home,
		Logger:    slog.New(signalHandler{msg: "codex app-server ended", ch: ended, once: new(sync.Once)}),
		Start: func() (appserver.Process, error) {
			p := fake.Process()
			mu.Lock()
			defer mu.Unlock()
			if first == nil {
				first = newGatedProc(p)
				return first, nil
			}
			return p, nil
		},
	})
	t.Cleanup(srv.Close)
	_, err := srv.Ensure(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fake.Crash(3)
	<-first.entered // the watcher has dropped the session and is stopping the dead process
	if _, err := srv.Ensure(context.Background()); err != nil {
		t.Fatalf("a call during the stop must start a new process: %v", err)
	}
	if srv.Status().State != appserver.StateRunning {
		t.Fatalf("status = %+v", srv.Status())
	}
	close(first.release)
	<-ended          // the watcher is about to record the end of the old process
	for range 1000 { // the record (if it wrongly happens) follows within moments
		if st := srv.Status(); st.State != appserver.StateRunning {
			t.Fatalf("the end of the old process overwrote the status of the live one: %+v", st)
		}
		runtime.Gosched()
	}
}

func TestStopTearsDownAStartThatIsStillInItsHandshake(t *testing.T) {
	fake := codextest.New(t, home)
	inHandshake, release := make(chan struct{}), make(chan struct{})
	fake.Override = map[string]func(json.RawMessage) codextest.Reply{
		protocol.MethodInitialize: func(json.RawMessage) codextest.Reply {
			close(inHandshake)
			<-release
			return codextest.Reply{Result: protocol.InitializeResult{CodexHome: home}}
		},
	}
	var proc *codextest.Process
	srv := appserver.New(appserver.Options{CodexHome: home, Start: func() (appserver.Process, error) {
		proc = fake.Process()
		return proc, nil
	}})
	t.Cleanup(srv.Close)
	errc := make(chan error, 1)
	go func() { _, err := srv.Ensure(context.Background()); errc <- err }()
	<-inHandshake
	srv.Stop() // an install or a logout: the files are about to change
	close(release)
	if err := <-errc; !errors.Is(err, appserver.ErrStart) {
		t.Fatalf("err = %v", err)
	}
	if srv.Running() || srv.Status().State != appserver.StateStopped {
		t.Fatalf("a start that Stop overtook must not come up: %+v", srv.Status())
	}
	<-proc.Done()
}

func TestTheReportedCodexHomeMayBeAnotherSpellingOfTheSameDirectory(t *testing.T) {
	real := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	other := filepath.Join(t.TempDir(), "codex-home") // exists, but is another directory
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		mam, reported string
		ok            bool
	}{
		"mam's path is a symlink":      {mam: link, reported: real, ok: true},
		"the server resolved a link":   {mam: real, reported: link, ok: true},
		"another directory":            {mam: real, reported: other, ok: false},
		"the server's path is missing": {mam: real, reported: filepath.Join(real, "nope"), ok: false},
	} {
		t.Run(name, func(t *testing.T) {
			fake := codextest.New(t, c.reported)
			srv := appserver.New(appserver.Options{CodexHome: c.mam, Start: func() (appserver.Process, error) { return fake.Process(), nil }})
			t.Cleanup(srv.Close)
			_, err := srv.Ensure(context.Background())
			if c.ok && err != nil {
				t.Fatalf("a directory that is the same must be accepted: %v", err)
			}
			if !c.ok && !errors.Is(err, appserver.ErrNotOurHome) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
