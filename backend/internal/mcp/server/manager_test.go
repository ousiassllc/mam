package server

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
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
	localErr error
	opened   []string
	logs     bytes.Buffer
}

func newManagerEnv(t *testing.T, preferredPort int) *managerEnv {
	t.Helper()
	e := &managerEnv{
		path:     filepath.Join(t.TempDir(), bind.FileName),
		local:    []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr(tailnetIP)},
		failOpen: map[string]error{},
	}
	e.m = NewManager(ManagerOptions{
		Logger:       slog.New(slog.NewTextHandler(&e.logs, nil)),
		Handler:      http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }),
		SettingsPath: e.path, PreferredPort: preferredPort,
		LocalAddrs: func() ([]netip.Addr, error) { return e.local, e.localErr },
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

// A failure nobody knows is not shown as the text of the error (#1010): the person reads the sentence of the unknown failure with an error ID,
// the original message is only in the log, and the log line has the same ID. This holds for every reason the saved address is not used.
func TestManager_StartTellsAnUnknownFailureWithoutItsRawMessage(t *testing.T) {
	unknown := apperr.New(apperr.CodeUnknown).View(apperr.LangJa).Sentence(apperr.LangJa)
	told := func(t *testing.T, e *managerEnv, raw string) {
		t.Helper()
		problem := e.m.Status().Problem
		assert.Contains(t, problem, unknown)
		assert.NotContains(t, problem, raw, "the original message is not on the screen")
		id := errorID(t, problem)
		assert.Contains(t, e.logs.String(), "error_id="+id)
		assert.Contains(t, e.logs.String(), raw, "the original message is in the log")
	}

	unreadable := newManagerEnv(t, 0)
	require.NoError(t, os.WriteFile(unreadable.path, []byte("{not json"), 0o600))
	require.NoError(t, unreadable.m.Start())
	assert.Contains(t, unreadable.m.Status().Problem, bind.FileName)
	told(t, unreadable, "invalid character")

	noInterfaces := newManagerEnv(t, 0)
	require.NoError(t, bind.Save(noInterfaces.path, tailnetIP))
	noInterfaces.localErr = errors.New("list network interfaces: route ip+net: netlink rib: permission denied")
	require.NoError(t, noInterfaces.m.Start())
	assert.Contains(t, noInterfaces.m.Status().Problem, tailnetIP)
	told(t, noInterfaces, "netlink rib")

	refusedBind := newManagerEnv(t, 0)
	require.NoError(t, bind.Save(refusedBind.path, tailnetIP))
	refusedBind.failOpen[tailnetIP] = errors.New("listen tcp: bind: cannot assign requested address")
	require.NoError(t, refusedBind.m.Start())
	assert.Equal(t, "127.0.0.1", hostOf(refusedBind.m.Addr()), "the loopback address is the fallback")
	told(t, refusedBind, "cannot assign requested address")

	dead := newManagerEnv(t, 0)
	dead.failOpen["127.0.0.1"] = errors.New("socket: too many open files")
	require.Error(t, dead.m.Start())
	told(t, dead, "too many open files")
}

// The failures of the machine are told in a sentence about this listener, not by errmap's table (whose remedies are about a vendor or the bundled ComfyUI):
// a real failed listen and a real unreadable mcp.json, the original message in the log only.
func TestManager_StartTellsARealListenOrReadFailureAboutTheListener(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })
	_, inUse := net.Listen("tcp", busy.Addr().String())
	require.Error(t, inUse)

	notAboutThisListener := func(t *testing.T, problem string) {
		t.Helper()
		assert.NotContains(t, problem, "ComfyUI", "a remedy about another feature")
		assert.NotContains(t, problem, "書き込め", "nothing is written here")
		assert.NotRegexp(t, `err_[0-9a-f-]+`, problem, "a known failure has no error ID")
	}

	dead := newManagerEnv(t, 0)
	dead.failOpen["127.0.0.1"] = fmt.Errorf("listen on 127.0.0.1: %w", inUse)
	require.Error(t, dead.m.Start())
	problem := dead.m.Status().Problem
	assert.Contains(t, problem, "ポートを開けませんでした")
	notAboutThisListener(t, problem)
	assert.Contains(t, dead.logs.String(), "address already in use", "the original message is in the log")
	assert.NotContains(t, problem, "address already in use")

	unreadable := newManagerEnv(t, 0)
	require.NoError(t, os.WriteFile(unreadable.path, []byte("{}"), 0o000))
	if _, err := os.ReadFile(unreadable.path); err == nil {
		t.Skip("the file can be read despite mode 0: the tests run as a user that ignores permissions")
	}
	require.NoError(t, unreadable.m.Start())
	problem = unreadable.m.Status().Problem
	assert.Contains(t, problem, bind.FileName)
	assert.Contains(t, problem, "ファイルを開けませんでした")
	notAboutThisListener(t, problem)
}

// A refusal of bind.Validate is already a sentence of its own (which address, and why it is not allowed): it is told as it is, with no error ID.
func TestManager_StartTellsTheRefusalOfAnAddressAsItsOwnSentence(t *testing.T) {
	e := newManagerEnv(t, 0)
	require.NoError(t, bind.Save(e.path, tailnetIP))
	e.local = []netip.Addr{netip.MustParseAddr("127.0.0.1")} // the address is not on this machine now
	require.NoError(t, e.m.Start())
	problem := e.m.Status().Problem
	assert.NotRegexp(t, `err_[0-9a-f-]+`, problem, "a refusal is not an unknown failure")
	assert.NotContains(t, e.logs.String(), "error_id=")
}

// errorID is the error ID (err_…) that the sentence tells.
func errorID(t *testing.T, sentence string) string {
	t.Helper()
	id := regexp.MustCompile(`err_[0-9a-f-]+`).FindString(sentence)
	require.NotEmpty(t, id, sentence)
	return id
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
