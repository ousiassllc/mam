package localruntime

import (
	"io"
	"os"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// logTailBytes is how much of the end of the log a View carries.
const logTailBytes = 4096

// fileSize is the size of the file at path (0 when it is not there).
func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

// logMark is where in the log file the latest start (or install) began: a View's LogTail is the file from there on.
type logMark struct {
	mu   sync.Mutex
	from int64
}

// markLog makes the log of the runtime begin here: the log file keeps what earlier runs wrote (it is appended to), and the LogTail of a View is
// the end of this run's output only (#1167).
func (i *instance) markLog() {
	n := fileSize(i.layout.LogPath())
	i.log.mu.Lock()
	i.log.from = n
	i.log.mu.Unlock()
}

// markingLog is build that notes where the log is before every start, so that what the child writes is the log of that start.
func (i *instance) markingLog(build func(port int) (proc.Spec, error)) func(port int) (proc.Spec, error) {
	return func(port int) (proc.Spec, error) {
		i.markLog()
		return build(port)
	}
}

// forgetFailure is for the install that replaced the files of the runtime: the failure of the files that are gone (Failed with its error, and the
// end of its log) is not shown against the new ones (model-manager.md §3.4, #1167).
func (i *instance) forgetFailure() {
	i.proc.ClearFailure()
	i.markLog()
}

// logTail is the end of the log of the current run ("" when it has written nothing, or the file cannot be read).
func (i *instance) logTail() string {
	i.log.mu.Lock()
	from := i.log.from
	i.log.mu.Unlock()
	return tailOf(i.layout.LogPath(), from)
}

// tailOf is the last bytes of the file at path as text, not before offset from ("" when it cannot be read). An offset past the end means the file was
// started again (it is cut when it grows too large): it is read from its beginning.
func tailOf(path string, from int64) string {
	f, err := os.Open(path) // #nosec G304 -- the runtime's own log path
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	start := int64(0)
	if st, err := f.Stat(); err == nil {
		if from <= st.Size() {
			start = from
		}
		start = max(start, st.Size()-logTailBytes)
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	b, _ := io.ReadAll(f)
	return strings.TrimSpace(strings.ToValidUTF8(string(b), ""))
}
