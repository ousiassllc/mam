//go:build windows

package fetch

import (
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// The Winsock errors for a connection that broke are the lost connection; a refusal, a full disk and a denied write are not.
func TestIsResetKnowsTheWinsockErrorsOfABrokenConnection(t *testing.T) {
	wrap := func(e error) error {
		return &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("wsarecv", e)}
	}
	for _, e := range resetErrnos {
		assert.True(t, isReset(e), e.Error())
		assert.True(t, isReset(wrap(e)), "wrapped: "+e.Error())
		assert.True(t, connectionLost(wrap(e)), "the connection is lost: "+e.Error())
	}
	for _, e := range []syscall.Errno{
		10061, // WSAECONNREFUSED
		5,     // ERROR_ACCESS_DENIED
		39,    // ERROR_HANDLE_DISK_FULL
		112,   // ERROR_DISK_FULL
	} {
		assert.False(t, isReset(e), e.Error())
		assert.False(t, isReset(wrap(e)), "wrapped: "+e.Error())
		assert.False(t, connectionLost(wrap(e)), "the connection is not lost: "+e.Error())
	}
	assert.False(t, isReset(nil))
}

// A full disk is what Windows says with ERROR_DISK_FULL (112) or ERROR_HANDLE_DISK_FULL (39), not ENOSPC: a write that fails
// with it is a failure of the local disk, which no new transfer can change.
func TestConnectionLostIgnoresWindowsDiskFull(t *testing.T) {
	for _, e := range []syscall.Errno{112, 39} {
		local := &fsutil.LocalError{Err: &os.PathError{Op: "write", Path: "m.safetensors.part", Err: e}}
		assert.False(t, connectionLost(local), e.Error())
	}
}
