// Package onboarding is the first-run guide (spec/components/onboarding-screen.md): whether the guide was already
// finished or skipped (a flag of its own, never guessed from the API keys or the other settings) and the facts the
// setup steps show about this machine (ffmpeg, the operating system). The guide saves nothing else: the keys, the
// LLM and the model assignments are written by the settings services behind the settings screen.
package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// FileName is the flag file in the data directory.
const FileName = "onboarding.json"

const keyCompleted = "completed"

// OS is the operating system the setup guidance is written for. The view (web/organisms/onboarding) has the same three
// values as a type of its own, because organisms do not import the service layer; the handler maps one to the other.
type OS string

// The systems the guide has its own text for; every other one is OSOther.
const (
	OSWindows OS = "windows"
	OSMac     OS = "macos"
	OSOther   OS = "other"
)

// FFmpeg is where ffmpeg and ffprobe were found; an empty path is a tool that was not found.
type FFmpeg struct {
	FFmpeg  string
	FFprobe string
}

// Found is whether both tools are there (what internal/media needs).
func (f FFmpeg) Found() bool { return f.FFmpeg != "" && f.FFprobe != "" }

// Service holds the flag file and reads the machine.
type Service struct {
	path     string
	lookPath func(file string) (string, error)
	goos     string
}

// Option replaces what the service reads from the machine (the tests).
type Option func(*Service)

// WithLookPath replaces how ffmpeg and ffprobe are looked for (the search of media/locate with no path set).
func WithLookPath(f func(string) (string, error)) Option { return func(s *Service) { s.lookPath = f } }

// WithGOOS replaces runtime.GOOS.
func WithGOOS(goos string) Option { return func(s *Service) { s.goos = goos } }

// New is the service whose flag is the file at path (FileName in the data directory).
func New(path string, opts ...Option) *Service {
	s := &Service{path: path, lookPath: func(name string) (string, error) { return locate.Find(name, "") }, goos: runtime.GOOS}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Completed is whether the guide was finished or skipped. A missing file is a first run; a file that cannot be read
// is an error (the caller decides what to do: the root page then shows the project list rather than a guide).
func (s *Service) Completed() (bool, error) {
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return false, err
	}
	raw, ok := obj[keyCompleted]
	if !ok {
		return false, nil
	}
	var done bool
	if err := json.Unmarshal(raw, &done); err != nil {
		return false, fmt.Errorf("%s: %q must be a boolean", s.path, keyCompleted)
	}
	return done, nil
}

// Complete sets the flag, keeping any other key of the file. It is the same when the flag is already set, and the flag is
// never cleared: opening the guide again from the settings does not make the root page show it again. A file that is not
// valid JSON (or not an object) holds nothing worth keeping and is the very thing that would keep the flag from being set:
// it is written over. A file that cannot be read for another reason (permissions) is an error.
func (s *Service) Complete() error {
	lock := settingsfile.Lock(s.path)
	defer lock()
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		if !isDamaged(err) {
			return err
		}
		obj = settingsfile.Object{}
	}
	obj[keyCompleted] = json.RawMessage("true")
	return settingsfile.Write(s.path, obj)
}

// isDamaged is whether err is settingsfile.Read refusing the content of the file (the JSON is invalid, or is not an object).
func isDamaged(err error) bool {
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &typed)
}

// OS is the operating system of this machine, for the text of the steps.
func (s *Service) OS() OS {
	switch s.goos {
	case "windows":
		return OSWindows
	case "darwin":
		return OSMac
	}
	return OSOther
}

// FFmpeg looks for ffmpeg and ffprobe the way internal/media does at startup (see WithLookPath). It runs neither.
func (s *Service) FFmpeg() FFmpeg {
	var f FFmpeg
	if p, err := s.lookPath(locate.FFmpeg); err == nil {
		f.FFmpeg = p
	}
	if p, err := s.lookPath(locate.FFprobe); err == nil {
		f.FFprobe = p
	}
	return f
}
