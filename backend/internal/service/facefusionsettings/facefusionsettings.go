// Package facefusionsettings is the "FaceFusion" section of the settings screen: the Python and the FaceFusion directory the user set, kept in
// facefusion.json of the data directory, and what the lookup (facefusion.Finder) finds with them. It is the counterpart of ffmpegsettings:
// a set path takes priority over the search; one that cannot be used is an error, never a silent fall back to another installation.
//
// The service reads the file on every call and nothing is built at start-up from it, so a saved path applies to the next swap at once
// (ffmpeg's applies from the next start because the app builds its tools then).
package facefusionsettings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// FileName is the settings file in the data directory.
const FileName = "facefusion.json"

// The keys of the file.
const (
	keyPython = "python"
	keyPath   = "path"
)

// Service reads and writes the file.
type Service struct {
	path   string
	finder facefusion.Finder
}

// Option replaces what the service reads from the machine (the tests).
type Option func(*Service)

// WithFinder replaces the Finder of the machine (facefusion.DefaultFinder).
func WithFinder(f facefusion.Finder) Option { return func(s *Service) { s.finder = f } }

// New is the service over the file at path (FileName in the data directory). home is the home directory the default places are under ("": none).
func New(path, home string, opts ...Option) *Service {
	s := &Service{path: path, finder: facefusion.DefaultFinder(home)}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Default is the service over facefusion.json of the data directory with the machine's places to search (the home directory's). It holds no
// state, so the one of the settings screen and the one the generation test runs with agree on the file.
func Default(dataDir string) *Service {
	home, _ := os.UserHomeDir() // "": the places under the home directory are not searched
	return New(filepath.Join(dataDir, FileName), home)
}

// Config is what the user set; an empty field is left to the search. A file that cannot be read or is not a JSON object is an error; a
// missing file is the empty Config.
func (s *Service) Config() (facefusion.Config, error) {
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return facefusion.Config{}, err
	}
	var cfg facefusion.Config
	for key, dst := range map[string]*string{keyPython: &cfg.Python, keyPath: &cfg.Path} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return facefusion.Config{}, fmt.Errorf("%s: %q must be a string", s.path, key)
		}
	}
	return cfg, nil
}

// Set stores the two paths, or clears one with "". Each must be usable (an executable file; a directory that has facefusion.py and the
// facefusion package), else the error is a *facefusion.ConfigError and nothing is written (the user's mistake). Keys of the file this
// service does not manage are kept.
func (s *Service) Set(cfg facefusion.Config) error {
	cfg.Python, cfg.Path = strings.TrimSpace(cfg.Python), strings.TrimSpace(cfg.Path)
	if cfg.Python != "" {
		if _, err := s.finder.Python(cfg.Python); err != nil {
			return err
		}
	}
	if cfg.Path != "" {
		if _, err := s.finder.Dir(cfg.Path); err != nil {
			return err
		}
	}
	defer settingsfile.Lock(s.path)()
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return err
	}
	for key, p := range map[string]string{keyPython: cfg.Python, keyPath: cfg.Path} {
		if p == "" {
			delete(obj, key)
			continue
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		obj[key] = raw
	}
	return settingsfile.Write(s.path, obj)
}

// Look finds the installation with the saved paths. The error wraps facefusion.ErrNotFound, or is a *facefusion.ConfigError for a saved
// path that cannot be used now (its file went away), or the reason the file cannot be read.
func (s *Service) Look() (facefusion.Install, error) {
	cfg, err := s.Config()
	if err != nil {
		return facefusion.Install{}, err
	}
	return s.finder.Look(cfg)
}

// Part is one of the two things the lookup finds: the Python and the FaceFusion directory.
type Part struct {
	// Configured is the path the user set ("" for none).
	Configured string
	// Found is what the lookup found; "" when Err is set.
	Found string
	// Err is why there is nothing: a *facefusion.ConfigError (the configured path cannot be used) or one that wraps facefusion.ErrNotFound.
	Err error
}

// View is the section as the screen shows it.
type View struct {
	Python, Path Part
	// Version and License are what the found FaceFusion says about itself ("" when it is not found or says nothing).
	Version, License string
	// SearchDirs are the directories searched for FaceFusion when no path is set, in order.
	SearchDirs []string
}

// Ready says both parts are found: a swap can be started.
func (v View) Ready() bool { return v.Python.Err == nil && v.Path.Err == nil }

// View reads the file and runs the lookup. An error is a file that cannot be read.
func (s *Service) View() (View, error) {
	cfg, err := s.Config()
	if err != nil {
		return View{}, err
	}
	v := View{Python: Part{Configured: cfg.Python}, Path: Part{Configured: cfg.Path}, SearchDirs: s.finder.Dirs}
	v.Python.Found, v.Python.Err = s.finder.Python(cfg.Python)
	v.Path.Found, v.Path.Err = s.finder.Dir(cfg.Path)
	if v.Path.Err == nil {
		v.Version, v.License = facefusion.ReadMetadata(v.Path.Found)
	}
	return v, nil
}
