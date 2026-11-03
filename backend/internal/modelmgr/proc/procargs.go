//go:build !windows

package proc

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// parseProcArgs2 reads the argv out of the buffer of the macOS sysctl kern.procargs2: an int32 argc, the executable path and its
// NUL padding, then argc NUL-terminated arguments (the environment follows and is ignored).
func parseProcArgs2(buf []byte) ([]string, error) {
	if len(buf) < 4 {
		return nil, errors.New("procargs2: too short")
	}
	argc := int(binary.NativeEndian.Uint32(buf[:4]))
	rest := buf[4:]
	_, rest, ok := bytes.Cut(rest, []byte{0}) // the executable path
	if !ok {
		return nil, errors.New("procargs2: no executable path")
	}
	rest = bytes.TrimLeft(rest, "\x00")
	argv := make([]string, 0, min(argc, len(buf)))
	for len(argv) < argc {
		arg, after, ok := bytes.Cut(rest, []byte{0})
		if !ok && len(arg) == 0 {
			return nil, errors.New("procargs2: fewer arguments than argc")
		}
		argv = append(argv, string(arg))
		rest = after
	}
	return argv, nil
}
