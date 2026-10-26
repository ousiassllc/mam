package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// rig is a Service over a fake app-server, with an executable of the user's choosing (a path).
type rig struct {
	t        *testing.T
	svc      *codex.Service
	fake     *codextest.Fake
	dataDir  string
	exe      string
	mu       sync.Mutex
	launches []appserver.Launch
	// wrap, when set (under mu), wraps each process the Service starts.
	wrap   func(*codextest.Process) appserver.Process
	opened []string
}

type rigOption func(*codex.Options)

func newRig(t *testing.T, opts ...rigOption) *rig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home) // nothing may be read or written below the user's own home
	t.Setenv("USERPROFILE", home)
	r := &rig{t: t, dataDir: filepath.Join(t.TempDir(), "data")}
	r.fake = codextest.New(t, filepath.Join(r.dataDir, "codex-home"))
	r.fake.Models = []protocol.Model{
		{ID: "m1", Model: "gpt-text", DisplayName: "Text only", InputModalities: []string{"text"}},
		{ID: "m2", Model: "gpt-vision", DisplayName: "Vision", IsDefault: true, InputModalities: []string{"text", "image"}},
		{ID: "m3", Model: "gpt-hidden", Hidden: true, InputModalities: []string{"text"}},
	}
	o := codex.Options{
		DataDir: r.dataDir, Version: "v0.0.1-test", Keys: credentialtest.New(),
		Open: func(url string) error { r.mu.Lock(); r.opened = append(r.opened, url); r.mu.Unlock(); return nil },
		Start: func(l appserver.Launch) (appserver.Process, error) {
			r.mu.Lock()
			r.launches = append(r.launches, l)
			wrap := r.wrap
			r.mu.Unlock()
			if wrap != nil {
				return wrap(r.fake.Process()), nil
			}
			return r.fake.Process(), nil
		},
	}
	for _, f := range opts {
		f(&o)
	}
	r.svc = codex.New(o)
	t.Cleanup(r.svc.Close)
	r.exe = filepath.Join(t.TempDir(), "codex-app-server-test")
	if err := os.WriteFile(r.exe, []byte("not a real executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetCustomPath(r.exe); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) launched() []appserver.Launch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]appserver.Launch(nil), r.launches...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func contains(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
