// Package ffmpegsettings is the "ffmpeg" section of the settings screen: the paths of ffmpeg and ffprobe the user set, kept in
// ffmpeg.json of the data directory, and what the lookup (spec/architecture/ffmpeg-lookup.md) finds with them. A set path
// takes priority over the search; one that cannot be used is an error, never a silent fall back to another ffmpeg.
//
// The service reads the file on every call, so it holds no state and any number of them over the same file agree. The tools
// the app built at startup do not change when the file does: a saved path applies from the next start.
package ffmpegsettings

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// FileName is the settings file in the data directory.
const FileName = "ffmpeg.json"

// The keys of the file.
const (
	keyFFmpeg  = "ffmpeg"
	keyFFprobe = "ffprobe"
)

// Service reads and writes the file.
type Service struct {
	path   string
	finder locate.Finder
}

// Option replaces what the service reads from the machine (the tests).
type Option func(*Service)

// WithFinder replaces the Finder of the machine (locate.DefaultFinder).
func WithFinder(f locate.Finder) Option { return func(s *Service) { s.finder = f } }

// New is the service over the file at path (FileName in the data directory).
func New(path string, opts ...Option) *Service {
	s := &Service{path: path, finder: locate.DefaultFinder()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Config is the paths the user set; an empty one is left to the search. A file that cannot be read or is not a JSON object
// is an error; a missing file is the empty Config.
func (s *Service) Config() (locate.Config, error) {
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return locate.Config{}, err
	}
	var cfg locate.Config
	for key, dst := range map[string]*string{keyFFmpeg: &cfg.FFmpeg, keyFFprobe: &cfg.FFprobe} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return locate.Config{}, fmt.Errorf("%s: %q must be a string", s.path, key)
		}
	}
	return cfg, nil
}

// Set stores the paths, or clears one with "". A path must be the absolute path of an executable file, else the error is a
// *locate.PathError and nothing is written (the user's mistake). Keys of the file this service does not manage are kept.
func (s *Service) Set(cfg locate.Config) error {
	cfg.FFmpeg, cfg.FFprobe = strings.TrimSpace(cfg.FFmpeg), strings.TrimSpace(cfg.FFprobe)
	for _, name := range []string{locate.FFmpeg, locate.FFprobe} {
		if p := cfg.Path(name); p != "" {
			if _, err := s.finder.Find(name, p); err != nil {
				return err
			}
		}
	}
	defer settingsfile.Lock(s.path)()
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return err
	}
	for key, p := range map[string]string{keyFFmpeg: cfg.FFmpeg, keyFFprobe: cfg.FFprobe} {
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

// Look finds ffmpeg and ffprobe with the saved paths (media.LookIn). The error wraps media.ErrUnavailable; for a path that
// cannot be used it also holds the *locate.PathError, and for a file that cannot be read the reason.
func (s *Service) Look() (media.Tools, error) {
	cfg, err := s.Config()
	if err != nil {
		return media.Tools{}, fmt.Errorf("%w: %w", media.ErrUnavailable, err)
	}
	return media.LookIn(s.finder, cfg)
}

// Find is where the executable called name is with the saved paths, the signature of exec.LookPath: the first-run guide
// reports it. An unreadable file leaves the paths to the search.
func (s *Service) Find(name string) (string, error) {
	cfg, _ := s.Config()
	return s.finder.Find(name, cfg.Path(name))
}

// Tool is one of the two executables as the lookup finds it with the saved paths.
type Tool struct {
	Name string
	// Configured is the path the user set ("" for none).
	Configured string
	// Path is the file the lookup found; "" when Err is set.
	Path string
	// Err is why there is no Path: a *locate.PathError (the configured path cannot be used) or one that wraps locate.ErrNotFound.
	Err error
}

// View is the section as the screen shows it.
type View struct {
	FFmpeg, FFprobe Tool
	// SearchDirs are the directories searched when a path is not set, in order.
	SearchDirs []string
}

// View reads the file and runs the lookup. An error is a file that cannot be read.
func (s *Service) View() (View, error) {
	cfg, err := s.Config()
	if err != nil {
		return View{}, err
	}
	tool := func(name string) Tool {
		t := Tool{Name: name, Configured: cfg.Path(name)}
		t.Path, t.Err = s.finder.Find(name, t.Configured)
		return t
	}
	return View{
		FFmpeg: tool(locate.FFmpeg), FFprobe: tool(locate.FFprobe),
		SearchDirs: locate.SearchDirs(s.finder.GOOS, s.finder.Path, s.finder.Extra),
	}, nil
}
