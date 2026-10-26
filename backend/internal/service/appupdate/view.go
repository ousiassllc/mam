package appupdate

import (
	"time"

	"github.com/ousiassllc/mam/backend/internal/service/updater"
)

// State is what the last check found, in the words of the settings screen.
type State string

// The states of View, in the order View picks them: a failed check shows as StateError whatever
// else is known, then the progress of a newer release, then the checks that found nothing to do.
const (
	// StateChecking: a check is running now (it downloads and verifies a newer installer).
	StateChecking State = "checking"
	// StateError: the last check failed; View.ErrorKind says how.
	StateError State = "error"
	// StateReady: a newer release was downloaded and verified; the app is about to restart into it.
	StateReady State = "ready"
	// StateBlocked: a newer release exists but work is running that a restart would lose; View.BlockedBy
	// says what. The check is repeated soon.
	StateBlocked State = "blocked"
	// StateAvailable: a newer release exists and is being fetched.
	StateAvailable State = "available"
	// StateDevBuild: a build from a working tree, which does not check.
	StateDevBuild State = "dev_build"
	// StateNoRelease: nothing is published in the release repository yet.
	StateNoRelease State = "no_release"
	// StateUpToDate: the running version is the latest.
	StateUpToDate State = "up_to_date"
	// StateUnchecked: no check has finished since the app started.
	StateUnchecked State = "unchecked"
)

// Block names the work that holds an installation back (View.BlockedBy).
type Block string

// The blocks. They are the updater's gate kinds, redeclared so that the web layer never imports the updater.
const (
	BlockJobs        Block = Block(updater.BlockJobs)
	BlockEditing     Block = Block(updater.BlockEditing)
	BlockMedia       Block = Block(updater.BlockMedia)
	BlockCheckFailed Block = Block(updater.BlockCheckFailed)
)

// ErrorKind says why the last check failed, without the raw error text (it is in the log).
type ErrorKind string

// The error kinds (the updater's, redeclared like Block).
const (
	ErrorNetwork      ErrorKind = ErrorKind(updater.ErrorNetwork)
	ErrorRateLimit    ErrorKind = ErrorKind(updater.ErrorRateLimit)
	ErrorAccess       ErrorKind = ErrorKind(updater.ErrorAccess)
	ErrorVerification ErrorKind = ErrorKind(updater.ErrorVerification)
	ErrorRelease      ErrorKind = ErrorKind(updater.ErrorRelease)
	ErrorOther        ErrorKind = ErrorKind(updater.ErrorOther)
)

// View is the state of the update as the settings screen shows it.
type View struct {
	// Version is the running version ("dev" for a build from a working tree).
	Version string
	State   State
	// CheckedAt is when the last check finished; zero before the first.
	CheckedAt time.Time
	// Latest is the tag of the newer release (StateAvailable, StateBlocked, StateReady, and StateError
	// when an earlier check had seen one).
	Latest string
	// BlockedBy is set with StateBlocked.
	BlockedBy Block
	// ErrorKind is set with StateError.
	ErrorKind ErrorKind
}

// viewOf maps the status of the Checker; checking is whether a check is running.
func viewOf(version string, st updater.Status, checking bool) View {
	v := View{Version: version, CheckedAt: st.CheckedAt}
	if st.Available {
		v.Latest = st.Version
	}
	switch {
	case checking:
		v.State = StateChecking
	case st.LastError != "":
		v.State, v.ErrorKind = StateError, ErrorKind(st.ErrorKind)
	case st.Ready:
		v.State = StateReady
	case st.Blocked:
		v.State, v.BlockedBy = StateBlocked, Block(st.BlockedKind)
	case st.Available:
		v.State = StateAvailable
	case st.DevBuild:
		v.State = StateDevBuild
	case st.NoRelease:
		v.State = StateNoRelease
	case st.CheckedAt.IsZero():
		v.State = StateUnchecked
	default:
		v.State = StateUpToDate
	}
	return v
}
