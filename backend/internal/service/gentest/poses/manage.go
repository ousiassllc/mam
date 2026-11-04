package poses

import (
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// Progress is how far a running install is: the bytes downloaded of the bytes the install still has to download (Total is 0 until the first
// report, and when everything was in place already).
type Progress struct {
	Running     bool
	Done, Total int64
}

// progress is the state of the install that Library.Progress reads while the install runs.
type progress struct {
	running     atomic.Bool
	done, total atomic.Int64
}

func (p *progress) start() { p.done.Store(0); p.total.Store(0); p.running.Store(true) }
func (p *progress) stop()  { p.running.Store(false) }
func (p *progress) set(done, total int64) {
	p.done.Store(done)
	p.total.Store(total)
}

// Progress says whether an install is running and how far it is. It never waits for the install.
func (l *Library) Progress() Progress {
	return Progress{Running: l.prog.running.Load(), Done: l.prog.done.Load(), Total: l.prog.total.Load()}
}

// Remove deletes the pictures, the pose catalog and what was downloaded, and the record of the install (the library is "not installed"
// again; the next install downloads it again), and the suggestive series with it (their names come from the catalog). A library that is not
// installed is removed without an error. A running install is a conflict: it is left to finish (or to be cancelled by the user leaving the page).
// Removing is allowed whatever the age gate says: clearing the files away is always possible.
func (l *Library) Remove() error {
	if l.prog.running.Load() {
		return gterr.Conflict("ポーズ集を導入している最中は削除できません。終わってからやり直してください")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.index, l.general = nil, nil
	if l.adult != nil {
		if err := l.adult.install.Remove(); err != nil {
			return err
		}
	}
	return l.install.Remove()
}
