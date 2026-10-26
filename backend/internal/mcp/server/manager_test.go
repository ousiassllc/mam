package server

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/bind"
)

const tailnetIP = "100.101.102.103"

// managerEnv is a Manager on this machine's loopback, pretending to own the tailnet address tailnetIP:
// the fake Listen binds the tailnet request on 127.0.0.2, which is loopback and free to take.
type managerEnv struct {
	m        *Manager
	path     string
	local    []netip.Addr
	failOpen map[string]error
	opened   []string
}

func newManagerEnv(t *testing.T, preferredPort int) *managerEnv {
	t.Helper()
	e := &managerEnv{
		path:     filepath.Join(t.TempDir(), bind.FileName),
		local:    []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr(tailnetIP)},
		failOpen: map[string]error{},
	}
	e.m = NewManager(ManagerOptions{
		Handler:      http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }),
		SettingsPath: e.path, PreferredPort: preferredPort,
		LocalAddrs: func() ([]netip.Addr, error) { return e.local, nil },
		Listen: func(network, address string) (net.Listener, error) {
			host, port, _ := net.SplitHostPort(address)
			e.opened = append(e.opened, host)
			if err := e.failOpen[host]; err != nil {
				return nil, err
			}
			if host == tailnetIP {
				host = "127.0.0.2"
			}
			return net.Listen(network, net.JoinHostPort(host, port))
		},
	})
	t.Cleanup(e.m.Close)
	return e
}

func reachable(addr string) bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + addr + "/")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func hostOf(addr string) string { h, _, _ := net.SplitHostPort(addr); return h }

func TestManager_StartsOnLoopbackByDefaultAndFallsBackToAnotherPortWhenBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()
	busyPort := busy.Addr().(*net.TCPAddr).Port

	e := newManagerEnv(t, busyPort)
	require.NoError(t, e.m.Start())

	st := e.m.Status()
	assert.Equal(t, "127.0.0.1", hostOf(st.Addr))
	assert.NotEqual(t, busyPort, e.m.listenPort(), "a busy preferred port falls back to the OS's choice")
	assert.Empty(t, st.Problem)
	assert.Equal(t, bind.Default, st.Saved)
	assert.Equal(t, []string{"127.0.0.1", tailnetIP}, st.Candidates)
	assert.True(t, reachable(st.Addr))
}

func (m *Manager) listenPort() int { return m.cur.ln.Addr().(*net.TCPAddr).Port }

func TestManager_RebindMovesTheListenerAndSavesTheChoice(t *testing.T) {
	e := newManagerEnv(t, 0)
	require.NoError(t, e.m.Start())
	old := e.m.Addr()

	require.NoError(t, e.m.Rebind(tailnetIP))

	now := e.m.Addr()
	assert.NotEqual(t, old, now)
	assert.Equal(t, "127.0.0.2", hostOf(now), "the fake Listen put the tailnet address here")
	assert.True(t, reachable(now))
	assert.Eventually(t, func() bool { return !reachable(old) }, 5*time.Second, 20*time.Millisecond, "the old listener is closed")
	got, err := bind.Load(e.path)
	require.NoError(t, err)
	assert.Equal(t, tailnetIP, got)
	assert.Equal(t, tailnetIP, e.m.Status().Saved)

	require.NoError(t, e.m.Rebind("127.0.0.1"), "and back")
	assert.Equal(t, "127.0.0.1", hostOf(e.m.Addr()))
	_, err = os.Stat(e.path)
	assert.ErrorIs(t, err, os.ErrNotExist, "the default is stored as no file")
}

func TestManager_AFailedRebindLeavesTheOldListenerAndTheSettingAlone(t *testing.T) {
	e := newManagerEnv(t, 0)
	require.NoError(t, e.m.Start())
	old := e.m.Addr()

	for name, host := range map[string]string{"all interfaces": "0.0.0.0", "lan": "192.168.1.5", "name": "tailnet.example"} {
		assert.ErrorIs(t, e.m.Rebind(host), bind.ErrRejected, name)
	}
	e.failOpen[tailnetIP] = errors.New("address already in use")
	assert.ErrorContains(t, e.m.Rebind(tailnetIP), "address already in use")

	assert.Equal(t, old, e.m.Addr())
	assert.True(t, reachable(old), "the old listener keeps serving")
	_, err := os.Stat(e.path)
	assert.ErrorIs(t, err, os.ErrNotExist, "nothing was saved")

	// A setting that cannot be written is a failed rebind too, and does not leave a second listener behind.
	delete(e.failOpen, tailnetIP)
	require.NoError(t, os.WriteFile(e.path+"-blocker", nil, 0o600))
	blocked := newManagerEnv(t, 0)
	blocked.m.opts.SettingsPath = filepath.Join(e.path+"-blocker", bind.FileName) // a directory that is a file
	blocked.local = e.local
	require.NoError(t, blocked.m.Start())
	before := blocked.m.Addr()
	assert.Error(t, blocked.m.Rebind(tailnetIP))
	assert.Equal(t, before, blocked.m.Addr())
	assert.True(t, reachable(before))
}

func TestManager_StartFallsBackToLoopbackWhenTheSavedAddressIsNotAvailable(t *testing.T) {
	e := newManagerEnv(t, 0)
	require.NoError(t, bind.Save(e.path, tailnetIP))
	e.local = []netip.Addr{netip.MustParseAddr("127.0.0.1")} // Tailscale is not up yet

	require.NoError(t, e.m.Start())

	assert.Equal(t, "127.0.0.1", hostOf(e.m.Addr()), "never a wider address than the person chose")
	st := e.m.Status()
	assert.Equal(t, tailnetIP, st.Saved, "the choice is kept for the next start")
	assert.Contains(t, st.Problem, tailnetIP)
	assert.NotContains(t, e.opened, tailnetIP, "the unavailable address is not even tried")
}

func TestManager_StartKeepsTheSavedAddressWhenItIsAvailableAndReadsAnUnreadableFileAsDefault(t *testing.T) {
	e := newManagerEnv(t, 0)
	require.NoError(t, bind.Save(e.path, tailnetIP))
	require.NoError(t, e.m.Start())
	assert.Equal(t, "127.0.0.2", hostOf(e.m.Addr()))
	assert.Empty(t, e.m.Status().Problem)

	broken := newManagerEnv(t, 0)
	require.NoError(t, os.WriteFile(broken.path, []byte("{not json"), 0o600))
	require.NoError(t, broken.m.Start())
	assert.Equal(t, "127.0.0.1", hostOf(broken.m.Addr()))
	assert.Contains(t, broken.m.Status().Problem, bind.FileName)
}

func TestManager_CloseStopsServingAndForbidsRebind(t *testing.T) {
	e := newManagerEnv(t, 0)
	require.NoError(t, e.m.Start())
	addr := e.m.Addr()

	e.m.Close()
	e.m.Close()

	assert.False(t, reachable(addr))
	assert.Empty(t, e.m.Addr())
	assert.Error(t, e.m.Rebind("127.0.0.1"))
}
