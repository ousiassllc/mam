package chatgpt_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/chatgpttest"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

// fixture is a Service over the fake server and an in-memory key store that enforces the Windows limit.
type fixture struct {
	t       *testing.T
	auth    *chatgpttest.Server
	keys    *credentialtest.Memory
	svc     *chatgpt.Service
	logs    *bytes.Buffer
	dataDir string
	lock    string
	now     atomic.Int64 // offset of the clock from the real time, in seconds
	opened  chan string
	// browser reacts to the authorization URL like a user who approves it.
	browser func(authURL string)
	// ports are the loopback ports the browser logins try, in order.
	ports []int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	// Any free port: the real 1455 and 1457 are shared by every process of the host, so a test must not depend on them.
	return newFixtureOn(t, []int{0})
}

// newFixtureOn is newFixture whose browser logins try callbackPorts in order.
func newFixtureOn(t *testing.T, callbackPorts []int) *fixture {
	t.Helper()
	f := &fixture{t: t, auth: chatgpttest.NewServer(t), keys: credentialtest.New(), logs: &bytes.Buffer{}, dataDir: t.TempDir(), opened: make(chan string, 4), ports: callbackPorts}
	f.keys.LimitBytes(2560)
	f.lock = t.TempDir() + "/mam/.auth-refresh.lock"
	f.browser = f.approve
	f.svc = f.newService()
	return f
}

// busyPort is a loopback port that stays taken until the test ends: the port that is busy "because another program
// uses it". The port is the test's own, so nothing else on the host can free or take it.
func busyPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// newService builds another Service over the same keys and lock, as another mam process of the same user would.
func (f *fixture) newService() *chatgpt.Service {
	return chatgpt.New(chatgpt.Options{
		Keys: f.keys, DataDir: f.dataDir, LockPath: f.lock, Issuer: f.auth.URL, CallbackPorts: f.ports,
		Logger: slog.New(slog.NewTextHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:    func() time.Time { return time.Now().Add(time.Duration(f.now.Load()) * time.Second) },
		Sleep:  func(context.Context, time.Duration) error { return nil },
		Open: func(u string) error {
			f.browser(u)
			return nil
		},
	})
}

// advance moves the clock of the Services.
func (f *fixture) advance(d time.Duration) { f.now.Add(int64(d / time.Second)) }

// approve is the browser of a user who signs in.
func (f *fixture) approve(authURL string) {
	select {
	case f.opened <- authURL:
	default:
	}
	f.auth.Approve(authURL)
}

// waitLogin polls a login until it leaves pending.
func waitLogin(t *testing.T, a *chatgpt.Account, id string) chatgpt.Login {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		l, ok := a.LoginStatus(id)
		require.True(t, ok)
		if l.State != chatgpt.LoginPending {
			return l
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.FailNow(t, "the login stayed pending")
	return chatgpt.Login{}
}

// everyKey is every stored value, for the grep tests.
func (f *fixture) leaks(output string) []string {
	var found []string
	output = chatgpttest.WithoutAttemptValues(output) // random base64url holds "eyJ" now and then
	for _, s := range []string{"-ACCESS-", "-REFRESH-", "secretsecret", "eyJ"} {
		if strings.Contains(output, s) {
			found = append(found, s)
		}
	}
	return found
}
