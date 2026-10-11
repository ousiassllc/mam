// Package escalation remembers which Judge result the automatic Repair loop has asked the user
// about, so that a loop that is looked at again by every job that ends does not ask twice for the
// same state.
package escalation

import "sync"

// Memory holds one Judge result ID per Shot: a newer result of the Shot replaces the older one, and
// Forget drops the Shot, so it holds only the Shots that wait for the user. The zero value is ready
// to use and safe for concurrent use.
type Memory struct {
	asked sync.Map // Shot ID -> ID of the Judge result the user was asked about
}

// Ask records that the user is being asked about the result of the Shot. It reports whether that
// same result had already been asked about.
func (m *Memory) Ask(shotID, resultID string) (already bool) {
	last, asked := m.asked.Swap(shotID, resultID)
	return asked && last == resultID
}

// Forget drops what is remembered of the Shot: the user answered, or the question never reached them.
func (m *Memory) Forget(shotID string) {
	m.asked.Delete(shotID)
}
