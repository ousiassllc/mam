//go:build windows

package fetch

import (
	"errors"
	"syscall"
)

// resetErrnos are the Winsock errors of a connection that was reset, aborted or lost: "wsarecv: An existing connection was
// forcibly closed by the remote host" is the first. (syscall defines no WSA* constants; these are their values.)
var resetErrnos = []syscall.Errno{
	10054, // WSAECONNRESET
	10053, // WSAECONNABORTED
	10052, // WSAENETRESET
	10050, // WSAENETDOWN
	10051, // WSAENETUNREACH
	10058, // WSAESHUTDOWN
	10060, // WSAETIMEDOUT
	10065, // WSAEHOSTUNREACH
}

// isReset reports whether err is the operating system saying that the connection was reset, aborted or lost.
func isReset(err error) bool {
	for _, e := range resetErrnos {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
