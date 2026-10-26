package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
)

// gatedProcess is a fake process whose Stop waits for the test once armed.
type gatedProcess struct {
	*codextest.Process
	armed   *atomic.Bool
	entered chan struct{} // receives once per Stop that reached the gate
	release chan struct{} // closed by the test
}

func (g *gatedProcess) Stop() {
	if g.armed.Load() {
		g.entered <- struct{}{}
		<-g.release
	}
	g.Process.Stop()
}

func TestAServiceUsesAnAbsoluteDataDirectoryWhateverItWasGiven(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(base) // the child runs in another directory: a relative CODEX_HOME would point elsewhere there
	home := filepath.Join(base, "rel-data", codexHomeName)
	fake := codextest.New(t, home) // Codex reports the absolute path
	var launch appserver.Launch
	svc := codex.New(codex.Options{
		DataDir: "rel-data", Version: "v0.0.1-test",
		Start: func(l appserver.Launch) (appserver.Process, error) { launch = l; return fake.Process(), nil },
	})
	t.Cleanup(svc.Close)
	exe := filepath.Join(t.TempDir(), "codex-app-server-test")
	if err := os.WriteFile(exe, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCustomPath(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("a relative data directory must work: %v", err)
	}
	if launch.CodexHome != home || !filepath.IsAbs(launch.WorkDir) || svc.Status().CodexHome != home {
		t.Errorf("launch = %+v, status home = %q", launch, svc.Status().CodexHome)
	}
}

func TestADataDirectoryBehindASymlinkIsAccepted(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "data-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	fake := codextest.New(t, filepath.Join(real, codexHomeName)) // Codex follows the link
	svc := codex.New(codex.Options{
		DataDir: link, Version: "v0.0.1-test",
		Start: func(appserver.Launch) (appserver.Process, error) { return fake.Process(), nil },
	})
	t.Cleanup(svc.Close)
	exe := filepath.Join(t.TempDir(), "codex-app-server-test")
	if err := os.WriteFile(exe, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCustomPath(exe); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("a data directory behind a symlink must work: %v", err)
	}
}

const codexHomeName = "codex-home"

func TestTwoInstallsCannotRunAtOnceAndTheEngineIsNotStartedWhileOneRuns(t *testing.T) {
	ir := newInstallRig(t)
	ir.install(t)
	var armed atomic.Bool
	gate := &gatedProcess{armed: &armed, entered: make(chan struct{}, 4), release: make(chan struct{})}
	ir.mu.Lock()
	ir.wrap = func(p *codextest.Process) appserver.Process {
		g := *gate
		g.Process = p
		return &g
	}
	ir.mu.Unlock()
	if _, err := ir.svc.Refresh(context.Background()); err != nil { // the engine is running
		t.Fatal(err)
	}
	armed.Store(true)
	first := make(chan error, 1)
	go func() { _, err := ir.svc.StartInstall(); first <- err }()
	<-gate.entered // the install is stopping the engine: the slow part between its check and its start

	if started, err := ir.svc.StartInstall(); started || err != nil {
		t.Errorf("a second install during the first: started = %v, err = %v", started, err)
	}
	if st := ir.svc.InstallStatus(); st.State != codex.InstallRunning {
		t.Errorf("install = %+v", st)
	}
	if _, err := ir.svc.Refresh(context.Background()); !errors.Is(err, codex.ErrEngineInstalling) {
		t.Errorf("the engine must not start while its files are replaced: %v", err)
	}
	if st := ir.svc.Status(); st.Engine != codex.EngineInstalling {
		t.Errorf("status = %+v", st)
	}
	close(gate.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	ir.svc.WaitInstall()
	st := ir.svc.Status()
	if st.Install.State != codex.InstallSucceeded || st.Engine != codex.EngineReady || st.EngineError != "" {
		t.Fatalf("status = %+v (a start refused during the install must leave no failure behind)", st)
	}
}

func TestARefusedInstallGivesTheStateBackAndAllowsANewOne(t *testing.T) {
	var free atomic.Uint64
	free.Store(1)
	ir := newInstallRig(t, func(o *codex.Options) {
		o.FreeBytes = func(string) (uint64, error) { return free.Load(), nil }
	})
	var nospace *fetch.NoSpaceError
	if _, err := ir.svc.StartInstall(); !errors.As(err, &nospace) {
		t.Fatalf("err = %v", err)
	}
	if st := ir.svc.Status(); st.Install.State == codex.InstallRunning || st.Engine != codex.EngineNotInstalled {
		t.Fatalf("a refused install must not look running: %+v", st)
	}
	ir.svc.WaitInstall() // nothing is left to wait for
	free.Store(1 << 40)
	ir.install(t)
	if st := ir.svc.Status(); st.Engine != codex.EngineReady {
		t.Errorf("status = %+v", st)
	}
}

// holdLog is a log handler that stops the goroutine that logs the message until the test releases it.
type holdLog struct {
	slog.Handler
	msg     string
	reached chan struct{}
	release chan struct{}
}

func (h holdLog) Enabled(context.Context, slog.Level) bool { return true }
func (h holdLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message == h.msg {
		h.reached <- struct{}{}
		<-h.release
	}
	return nil
}

func TestALogoutWaitsForTheRefreshesAlreadyStartedAndNothingRestartsOrRecreatesTheFiles(t *testing.T) {
	// The refresh the notification starts is held where it ends (the log of its failed read, which the logout's stop
	// of the process causes): a logout that did not wait for it would be done by then.
	hold := holdLog{msg: "codex: account/rateLimits/read failed", reached: make(chan struct{}, 1), release: make(chan struct{})}
	ir := newInstallRig(t, func(o *codex.Options) { o.Logger = slog.New(hold) })
	ir.install(t)
	if _, err := ir.svc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	var armed atomic.Bool
	inRead, release := make(chan struct{}, 1), make(chan struct{})
	ir.fake.Override = map[string]func(json.RawMessage) codextest.Reply{
		protocol.MethodRateLimitsRead: func(json.RawMessage) codextest.Reply {
			if armed.Load() {
				inRead <- struct{}{}
				<-release
			}
			return codextest.Reply{Result: protocol.RateLimitsResult{}}
		},
	}
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(hold.release); close(release) }) }
	t.Cleanup(releaseAll) // a failed test must not leave Close waiting for a held refresh
	armed.Store(true)
	ir.fake.Notify(protocol.NotifAccountUpdated, map[string]any{"authMode": "chatgpt", "planType": "business"})
	<-inRead // the refresh started by the notification is in the middle of its work

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := ir.svc.Logout(context.Background(), true); err != nil {
			t.Error(err)
		}
	}()
	<-hold.reached // Logout stopped the process; the refresh ends (held)
	// Without the wait, the logout goes on to remove the record, and that is seen here (a held logout never gets there;
	// the rounds are system calls, far more than the work the logout has left).
	state := filepath.Join(ir.dataDir, codex.StateFile)
	for range 100000 {
		select {
		case <-done:
			t.Fatal("Logout returned while a refresh it should wait for was still running")
		default:
		}
		if _, err := os.Stat(state); err != nil {
			t.Fatal("Logout went on to remove its files while a refresh it should wait for was still running")
		}
	}
	releaseAll()
	<-done
	if n := len(ir.launched()); n != 1 {
		t.Errorf("a refresh must not start the app-server again: launches = %d", n)
	}
	for _, gone := range []string{filepath.Join(ir.dataDir, codexHomeName), filepath.Join(ir.dataDir, "codex-work"), filepath.Join(ir.dataDir, codex.StateFile)} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s is back after the logout", gone)
		}
	}
}
