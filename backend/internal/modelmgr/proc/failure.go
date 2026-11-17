package proc

// storeLocked stores s and returns the watchers to tell (the caller holds m.mu and calls them after it let go).
func (m *Manager) storeLocked(s Status) []func(Status) {
	m.status = s
	fns := make([]func(Status), 0, len(m.watchers))
	for _, fn := range m.watchers {
		fns = append(fns, fn)
	}
	return fns
}

// ClearFailure forgets a failed start or exit: Failed becomes Stopped, with no error and no end of the log, and the watchers are told. It is for
// the caller that replaced the files of the child, so that the failure of the files that are gone is not shown against the new ones
// (model-manager.md §3.3). A start in progress and every other state are left alone.
func (m *Manager) ClearFailure() {
	m.mu.Lock()
	if m.status.State != StateFailed || m.starting != nil {
		m.mu.Unlock()
		return
	}
	s := Status{State: StateStopped}
	fns := m.storeLocked(s)
	m.mu.Unlock()
	for _, fn := range fns {
		fn(s)
	}
}
