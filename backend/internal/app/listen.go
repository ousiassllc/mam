package app

import (
	"fmt"
	"net"
	"strconv"
)

// loopbackHost is the only address the server ever binds to (non-functional.md §5).
const loopbackHost = "127.0.0.1"

// listenLoopback binds 127.0.0.1:preferred. If that port is in use (or preferred
// is 0) it falls back to a port chosen by the OS. Binding the socket directly,
// instead of probing for a free port first, avoids a race with other processes.
func listenLoopback(preferred int) (net.Listener, error) {
	if preferred > 0 {
		if ln, err := net.Listen("tcp4", net.JoinHostPort(loopbackHost, strconv.Itoa(preferred))); err == nil {
			return ln, nil
		}
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort(loopbackHost, "0"))
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", loopbackHost, err)
	}
	return ln, nil
}

// baseURL is the address to open in the browser for ln.
func baseURL(ln net.Listener) string {
	return "http://" + ln.Addr().String()
}
