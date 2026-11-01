//go:build !windows

package fetch

import (
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The errors the operating system gives for a connection that broke are the lost connection; a refusal is not.
func TestConnectionLostKnowsTheErrorsOfABrokenConnection(t *testing.T) {
	wrap := func(e error) error { return &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", e)} }
	for _, e := range []error{syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE, syscall.ETIMEDOUT} {
		assert.True(t, connectionLost(wrap(e)), e.Error())
	}
	for _, e := range []error{syscall.ECONNREFUSED, syscall.ENOSPC, syscall.EACCES} {
		assert.False(t, connectionLost(wrap(e)), e.Error())
	}
	assert.False(t, connectionLost(nil))
}
