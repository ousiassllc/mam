//go:build !windows

package fetch

import (
	"errors"
	"syscall"
)

// isReset reports whether err is the operating system saying that the connection was reset, aborted or lost.
func isReset(err error) bool {
	for _, e := range []error{syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE, syscall.ETIMEDOUT, syscall.ENETRESET, syscall.ENETDOWN, syscall.ENETUNREACH, syscall.EHOSTUNREACH} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
