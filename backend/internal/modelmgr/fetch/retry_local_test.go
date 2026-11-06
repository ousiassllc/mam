package fetch

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// A write that fails because the disk is full is a failure of the local disk: it is not a lost connection, so the download is
// not resumed (a new transfer would fill the same disk).
func TestConnectionLostIgnoresAFullDisk(t *testing.T) {
	full := &fsutil.LocalError{Err: &os.PathError{Op: "write", Path: "m.safetensors.part", Err: syscall.ENOSPC}}
	assert.False(t, connectionLost(full))
	assert.False(t, connectionLost(&NoSpaceError{Path: "m.safetensors", Need: 2, Free: 1}))
}
