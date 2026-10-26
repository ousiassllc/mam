package bind_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/bind"
)

var machine = []netip.Addr{
	netip.MustParseAddr("127.0.0.1"),
	netip.MustParseAddr("::1"),
	netip.MustParseAddr("192.168.1.20"),
	netip.MustParseAddr("100.101.102.103"),
	netip.MustParseAddr("fd7a:115c:a1e0::1"),
}

func TestValidate_AcceptsLoopbackAndTailnetAddressesOfTheMachine(t *testing.T) {
	for _, host := range []string{"127.0.0.1", " 127.0.0.1 ", "::1", "100.101.102.103"} {
		ip, err := bind.Validate(host, machine)
		require.NoError(t, err, host)
		assert.Equal(t, netip.MustParseAddr(strings.TrimSpace(host)), ip)
	}
}

func TestValidate_RejectsEverythingElse(t *testing.T) {
	for name, tc := range map[string]string{
		"empty":                   "",
		"all interfaces":          "0.0.0.0",
		"all interfaces ipv6":     "::",
		"lan address":             "192.168.1.20",
		"private 10/8":            "10.0.0.5",
		"public address":          "8.8.8.8",
		"host name":               "localhost",
		"tailnet name":            "host.tailnet.ts.net",
		"outside 100.64.0.0/10":   "100.128.0.1",
		"just below the range":    "100.63.255.255",
		"ipv6 tailscale ULA":      "fd7a:115c:a1e0::1",
		"with port":               "127.0.0.1:8788",
		"loopback not on the box": "127.0.0.2",
		"tailnet not on the box":  "100.64.0.9",
	} {
		_, err := bind.Validate(tc, machine)
		assert.ErrorIs(t, err, bind.ErrRejected, name)
	}
}

func TestValidate_TailnetAddressNeedsAnInterface(t *testing.T) {
	_, err := bind.Validate("100.101.102.103", []netip.Addr{netip.MustParseAddr("127.0.0.1")})
	require.ErrorIs(t, err, bind.ErrRejected)
	assert.Contains(t, err.Error(), "インターフェース")
}

func TestCandidates_ListsLoopbackAndTailnetAddresses(t *testing.T) {
	assert.Equal(t, []string{"127.0.0.1", "100.101.102.103"}, bind.Candidates(machine))
}

func TestLoadSave_KeepsUnknownKeysAndDefaultRemovesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", bind.FileName)

	got, err := bind.Load(path)
	require.NoError(t, err)
	assert.Equal(t, bind.Default, got, "no file: the default")

	require.NoError(t, bind.Save(path, "100.101.102.103"))
	got, err = bind.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "100.101.102.103", got)

	require.NoError(t, os.WriteFile(path, []byte(`{"bind_address":"100.101.102.103","hand_written":1}`), 0o600))
	require.NoError(t, bind.Save(path, bind.Default))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"hand_written":1}`, string(data), "the other key survives")

	require.NoError(t, os.WriteFile(path, []byte(`{"bind_address":5}`), 0o600))
	_, err = bind.Load(path)
	assert.Error(t, err)
}
