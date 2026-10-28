// Package onboarding is the first-run guide (spec/components/onboarding-screen.md): whether the guide was already
// finished or skipped (a flag of its own, never guessed from the API keys or the other settings) and the facts the
// setup steps show about this machine (ffmpeg, the operating system). The guide saves nothing else: the keys, the
// LLM and the model assignments are written by the settings services behind the settings screen.
package onboarding

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// FileName is the flag file in the data directory.
const FileName = "onboarding.json"

const keyCompleted = "completed"

// OS is the operating system the setup guidance is written for.
type OS string

// The systems the guide has its own text for; every other one is OSOther.
const (
	OSWindows OS = "windows"
	OSMac     OS = "macos"
	OSOther   OS = "other"
)

// FFmpeg is where ffmpeg and ffprobe were found on the PATH; an empty path is a tool that was not found.
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

// WithLookPath replaces exec.LookPath.
func WithLookPath(f func(string) (string, error)) Option { return func(s *Service) { s.lookPath = f } }

// WithGOOS replaces runtime.GOOS.
func WithGOOS(goos string) Option { return func(s *Service) { s.goos = goos } }

// New is the service whose flag is the file at path (FileName in the data directory).
func New(path string, opts ...Option) *Service {
	s := &Service{path: path, lookPath: exec.LookPath, goos: runtime.GOOS}
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
// never cleared: opening the guide again from the settings does not make the root page show it again.
func (s *Service) Complete() error {
	lock := settingsfile.Lock(s.path)
	defer lock()
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return err
	}
	obj[keyCompleted] = json.RawMessage("true")
	return settingsfile.Write(s.path, obj)
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

// FFmpeg looks for ffmpeg and ffprobe on the PATH, the way internal/media does at startup. It runs neither.
func (s *Service) FFmpeg() FFmpeg {
	var f FFmpeg
	if p, err := s.lookPath("ffmpeg"); err == nil {
		f.FFmpeg = p
	}
	if p, err := s.lookPath("ffprobe"); err == nil {
		f.FFprobe = p
	}
	return f
}
