package facemodels

import (
	"sync/atomic"

	"github.com/ousiassllc/mam/backend/internal/facesim"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
)

// State is whether the models are in the model folder.
type State string

// The states of the models.
const (
	// StateInstalled: both files are there and were checked against the pinned SHA-256.
	StateInstalled State = "installed"
	// StateUnverified: both files are there with the pinned sizes but mam has not checked their SHA-256 (put there by hand, scripts/fetch-face-models.sh,
	// or an earlier install whose record is gone). The engine loads them all the same; "installing" checks them and keeps what is right.
	StateUnverified State = "unverified"
	// StateMissing: a file is not there (or is not the pinned size).
	StateMissing State = "missing"
)

// File is one model file.
type File struct {
	// Name is the file name in the model folder; Title and Description say what it is.
	Name, Title, Description string
	Size                     int64
	State                    State
	// SourceName and SourceURL are where the original is.
	SourceName, SourceURL string
}

// Status is what the user is told: where the files come from, how large they are, the licenses with their commercial-use answers and the state.
type Status struct {
	// ModelDir is the folder the files are installed to and read from.
	ModelDir string
	Files    []File
	// State is StateInstalled when every file is, StateMissing when any is missing, else StateUnverified.
	State State
	// DownloadBytes is the size of everything an install downloads, RemainBytes how much is still to fetch (less when files are in place already),
	// NeedBytes the free space an install needs.
	DownloadBytes, RemainBytes, NeedBytes int64
	// Licenses are the licenses of the set, each with its commercial-use answer and the notice.
	Licenses []catalog.ResourceLicense
	// Off is why the face engine is off right now, nil when it works. It is set while the files are in place (the ONNX Runtime library is what is
	// missing then: installing the models does not fix that) and while they are not (facesim.ReasonModelsMissing).
	Off *facesim.Off
}

// Installed reports whether both files are in the model folder and checked.
func (s Status) Installed() bool { return s.State == StateInstalled }

// Status reads the disk (the record of verified files, then the files) and asks the engine why it is off.
func (s *Service) Status() Status {
	st := Status{ModelDir: s.ModelDir(), State: StateInstalled, DownloadBytes: s.set.TotalSize(), NeedBytes: s.install.AssetNeedBytes(), Licenses: s.set.Licenses}
	for i, a := range s.install.Assets() {
		asset := s.set.Assets[i]
		state := StateInstalled
		switch a.State {
		case rtm.AssetMissing:
			state = StateMissing
			st.RemainBytes += asset.Size
		case rtm.AssetUnverified:
			state = StateUnverified
		}
		if state == StateMissing || (state == StateUnverified && st.State == StateInstalled) {
			st.State = state
		}
		st.Files = append(st.Files, File{
			Name: asset.Dest, Title: asset.Name, Description: asset.Description, Size: asset.Size, State: state,
			SourceName: asset.Source.Name, SourceURL: asset.Source.URL,
		})
	}
	if s.engine != nil {
		st.Off = s.engine.Off()
	}
	return st
}

// Progress is how far a running install is: the bytes downloaded of the bytes the install still has to download (Total is 0 until the first report, and
// when everything was in place already).
type Progress struct {
	Running     bool
	Done, Total int64
}

// progress is the state of the install that Service.Progress reads while the install runs.
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
func (s *Service) Progress() Progress {
	return Progress{Running: s.prog.running.Load(), Done: s.prog.done.Load(), Total: s.prog.total.Load()}
}
