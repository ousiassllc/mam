// Package desktop holds the parts of the Wails front (cmd/wails) that do not depend on Wails, so
// that they build and are tested on every OS: the gate that loads the server URL into the window,
// and the background server with its graceful stop.
package desktop

import "sync"

// Navigator loads the server URL into the window once both are ready. The server reports its URL
// (SetURL) and the window finishes its first navigation (WindowReady) in either order, on
// different goroutines; the window is never told to navigate before it can.
type Navigator struct {
	navigate func(url string)

	mu    sync.Mutex
	url   string
	ready bool
}

// NewNavigator returns a Navigator that calls navigate with the server URL.
func NewNavigator(navigate func(url string)) *Navigator {
	return &Navigator{navigate: navigate}
}

// SetURL records the server URL and navigates to it if the window is ready.
func (n *Navigator) SetURL(url string) {
	n.mu.Lock()
	n.url = url
	ready := n.ready
	n.mu.Unlock()
	if ready {
		n.navigate(url)
	}
}

// WindowReady marks the window as able to navigate and navigates to the server URL if it is known.
// Only the first call has an effect: later ones are the navigation events of the loaded app.
func (n *Navigator) WindowReady() {
	n.mu.Lock()
	first := !n.ready
	n.ready = true
	url := n.url
	n.mu.Unlock()
	if first && url != "" {
		n.navigate(url)
	}
}
