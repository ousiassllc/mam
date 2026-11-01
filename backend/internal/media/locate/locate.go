// Package locate finds the ffmpeg and ffprobe executables (spec/architecture/ffmpeg-lookup.md): the path the user set in
// the settings wins, else PATH is searched, then the directories Homebrew installs to on macOS. It is a package of its own
// (internal/media imports it) so that the test helpers, which internal/media's own tests import, can use it too.
package locate

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// The names of the two executables.
const (
	FFmpeg  = "ffmpeg"
	FFprobe = "ffprobe"
)

// ErrNotFound means an executable is in none of the places searched.
var ErrNotFound = errors.New("not found")

// Config is the executables the user chose in the settings. A set path takes priority over any search; an empty one
// leaves the executable to the search.
type Config struct {
	FFmpeg  string `json:"ffmpeg,omitempty"`
	FFprobe string `json:"ffprobe,omitempty"`
}

// Path is the configured path of the executable called name ("" for a name that is neither of the two).
func (c Config) Path(name string) string {
	switch name {
	case FFmpeg:
		return c.FFmpeg
	case FFprobe:
		return c.FFprobe
	}
	return ""
}

// The reasons a configured path cannot be used (the Err of a PathError).
var (
	ErrNotAbsolute   = errors.New("it must be an absolute path")
	ErrNotExecutable = errors.New("it is not an executable file")
)

// PathError is a configured path that cannot be used. It is reported as it is: the search does not take over, since that
// would run another executable than the one the user chose.
type PathError struct {
	Name string // FFmpeg or FFprobe
	Path string
	Err  error // ErrNotAbsolute or ErrNotExecutable
}

func (e *PathError) Error() string {
	return fmt.Sprintf("the %s path %q cannot be used: %v", e.Name, e.Path, e.Err)
}

func (e *PathError) Unwrap() error { return e.Err }

// wellKnownDirs are the directories searched after PATH on goos. A macOS app started from the Finder or the Dock gets the
// PATH of launchd (/usr/bin:/bin:/usr/sbin:/sbin), which holds neither of the places Homebrew installs to
// (/opt/homebrew/bin on Apple silicon, /usr/local/bin on Intel). The login shell's PATH is deliberately not read.
func wellKnownDirs(goos string) []string {
	if goos == "darwin" {
		return []string{"/opt/homebrew/bin", "/usr/local/bin"}
	}
	return nil
}

// SearchDirs is the directories searched for an executable, in order: the entries of pathEnv (the value of PATH), then the
// well-known install directories of goos, then extra (places of mam's own, such as the directory a downloaded ffmpeg lies
// in). Empty and relative entries are left out (a relative one would make the result depend on the working directory) and
// so are repeats. It reads neither the environment nor the disk.
func SearchDirs(goos, pathEnv string, extra []string) []string {
	sep := ":"
	if goos == "windows" {
		sep = ";"
	}
	candidates := append(strings.Split(pathEnv, sep), wellKnownDirs(goos)...)
	candidates = append(candidates, extra...)
	var dirs []string
	seen := map[string]bool{}
	for _, d := range candidates {
		if goos == "windows" {
			d = strings.Trim(d, `"`) // PATH may quote an entry
		}
		if d == "" || !isAbs(goos, d) || seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	return dirs
}

// isAbs is whether p is an absolute path on goos (filepath.IsAbs knows only the host's rules).
func isAbs(goos, p string) bool {
	if goos != "windows" {
		return strings.HasPrefix(p, "/")
	}
	if strings.HasPrefix(p, `\\`) {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z')
}

// Candidates are the files that the executable called name may be, in dirs order: name in every directory, with the ".exe"
// suffix on Windows.
func Candidates(name, goos string, dirs []string) []string {
	file, sep := name, "/"
	if goos == "windows" {
		file, sep = file+".exe", `\`
	}
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = strings.TrimRight(d, sep) + sep + file
	}
	return out
}

// Finder finds executables. DefaultFinder is the machine's.
type Finder struct {
	// GOOS decides the well-known directories, the path separator of Path and the suffix of the file names.
	GOOS string
	// Path is the value of the PATH environment variable.
	Path string
	// Extra are directories searched after PATH and the well-known ones (SearchDirs).
	Extra []string
	// Usable is whether the file at the path can be run; nil: nothing can.
	Usable func(path string) bool
}

// DefaultFinder is the Finder of this machine: its PATH, and a file is usable when it is a regular file that may be run.
func DefaultFinder() Finder {
	return Finder{GOOS: runtime.GOOS, Path: os.Getenv("PATH"), Usable: IsExecutable}
}

// IsExecutable is whether path is a regular file (a link is followed) that may be run. Windows has no execute bit.
func IsExecutable(path string) bool {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || st.Mode().Perm()&0o111 != 0
}

func (f Finder) usable(path string) bool { return f.Usable != nil && f.Usable(path) }

// Find is where the executable called name is. A configured path wins and is the only one tried: a path that cannot be
// used is a *PathError. Without one, the directories of SearchDirs are searched in order; none holding it is an error that
// wraps ErrNotFound and lists the directories.
func (f Finder) Find(name, configured string) (string, error) {
	if configured != "" {
		switch {
		case !isAbs(f.GOOS, configured):
			return "", &PathError{Name: name, Path: configured, Err: ErrNotAbsolute}
		case !f.usable(configured):
			return "", &PathError{Name: name, Path: configured, Err: ErrNotExecutable}
		}
		return configured, nil
	}
	dirs := SearchDirs(f.GOOS, f.Path, f.Extra)
	for _, c := range Candidates(name, f.GOOS, dirs) {
		if f.usable(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s %w (searched %s)", name, ErrNotFound, strings.Join(dirs, ", "))
}

// Find is Finder.Find of DefaultFinder.
func Find(name, configured string) (string, error) {
	return DefaultFinder().Find(name, configured)
}
