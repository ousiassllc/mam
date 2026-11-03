package facefusion

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

// Script is the entry point of FaceFusion in its directory; MetadataFile is the file that holds its version and license.
const (
	Script       = "facefusion.py"
	MetadataFile = "facefusion/metadata.py"
)

// The reasons a configured path cannot be used (the Err of a ConfigError), besides locate.ErrNotAbsolute and locate.ErrNotExecutable.
var (
	// ErrNoScript: the directory has no facefusion.py.
	ErrNoScript = errors.New("facefusion.py is not in it")
	// ErrNotFaceFusion: facefusion.py is there but the facefusion package next to it is not (another program's script).
	ErrNotFaceFusion = errors.New("it is not a FaceFusion directory (facefusion/metadata.py is missing)")
	// ErrNotFound: nothing was configured and nothing was found in the places searched.
	ErrNotFound = errors.New("not found")
)

// Config is what the user set: the Python that runs FaceFusion (the one of its Miniconda environment) and the FaceFusion directory
// (the one that holds facefusion.py; the path of facefusion.py itself is taken as its directory). An empty field is left to the search.
type Config struct {
	Python string `json:"python,omitempty"`
	Path   string `json:"path,omitempty"`
}

// The names of the two settings (ConfigError.Field).
const (
	FieldPython = "python"
	FieldPath   = "path"
)

// ConfigError is a configured path that cannot be used. It is reported as it is: the search does not take over, since that would run
// another program than the one the user chose.
type ConfigError struct {
	Field string // FieldPython or FieldPath
	Path  string
	Err   error
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("the %s path %q cannot be used: %v", e.Field, e.Path, e.Err)
}

func (e *ConfigError) Unwrap() error { return e.Err }

// Install is a FaceFusion that can be started.
type Install struct {
	// Python is the executable that runs it; Dir is the directory of facefusion.py (the working directory of a run).
	Python string
	Dir    string
	// Version and License are read from facefusion/metadata.py ("" when it does not say).
	Version string
	License string
}

// Finder is what Look reads from the machine; locate.DefaultFinder() is the real one. Dirs are the directories searched for FaceFusion when
// no path is set (the first one that has facefusion.py); Pythons are the files tried for the Python before PATH is (the places a conda
// environment named facefusion puts its python).
type Finder struct {
	locate.Finder
	Dirs    []string
	Pythons []string
}

// pythonNames are the executables searched for when no Python is set, in order. On Windows locate adds ".exe".
var pythonNames = []string{"python3", "python"}

// Look finds the Python and the FaceFusion directory with the configured paths and, for an empty one, the search. A configured path
// that cannot be used is a *ConfigError; nothing found is an error that wraps ErrNotFound and says where it looked. The directory is
// checked to be FaceFusion's, but FaceFusion is not run (starting it imports its models' runtime and takes seconds).
func (f Finder) Look(cfg Config) (Install, error) {
	py, err := f.Python(strings.TrimSpace(cfg.Python))
	if err != nil {
		return Install{}, err
	}
	dir, err := f.Dir(strings.TrimSpace(cfg.Path))
	if err != nil {
		return Install{}, err
	}
	in := Install{Python: py, Dir: dir}
	in.Version, in.License = ReadMetadata(dir)
	return in, nil
}

// Python is the executable that runs FaceFusion: the configured path (a *ConfigError when it cannot be used), else the first of Pythons that
// can be run, else python3 / python on PATH.
func (f Finder) Python(configured string) (string, error) {
	if configured != "" {
		p, err := f.Find("python", configured)
		var pe *locate.PathError
		if errors.As(err, &pe) {
			return "", &ConfigError{Field: FieldPython, Path: pe.Path, Err: pe.Err}
		}
		return p, err
	}
	for _, p := range f.Pythons {
		if f.Usable != nil && f.Usable(p) {
			return p, nil
		}
	}
	for _, name := range pythonNames {
		if p, err := f.Find(name, ""); err == nil {
			return p, nil
		}
	}
	dirs := locate.SearchDirs(f.GOOS, f.Path, f.Extra)
	return "", fmt.Errorf("python %w (searched %s for %s)", ErrNotFound, strings.Join(dirs, ", "), strings.Join(pythonNames, ", "))
}

// Dir is the FaceFusion directory: the configured path (a facefusion.py is taken as its directory; a *ConfigError when it cannot be used),
// else the first of Dirs that has one.
func (f Finder) Dir(configured string) (string, error) {
	if configured != "" {
		dir := configured
		if strings.EqualFold(filepath.Base(configured), Script) {
			dir = filepath.Dir(configured)
		}
		if !filepath.IsAbs(dir) {
			return "", &ConfigError{Field: FieldPath, Path: configured, Err: locate.ErrNotAbsolute}
		}
		if err := checkDir(dir); err != nil {
			return "", &ConfigError{Field: FieldPath, Path: configured, Err: err}
		}
		return dir, nil
	}
	for _, d := range f.Dirs {
		if checkDir(d) == nil {
			return d, nil
		}
	}
	return "", fmt.Errorf("facefusion %w (searched %s)", ErrNotFound, strings.Join(f.Dirs, ", "))
}

// checkDir says whether dir is a FaceFusion directory: ErrNoScript when facefusion.py is not in it, ErrNotFaceFusion when the package is not.
func checkDir(dir string) error {
	if st, err := os.Stat(filepath.Join(dir, Script)); err != nil || !st.Mode().IsRegular() {
		return ErrNoScript
	}
	if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(MetadataFile))); err != nil || !st.Mode().IsRegular() {
		return ErrNotFaceFusion
	}
	return nil
}

var (
	versionRe = regexp.MustCompile(`'version'\s*:\s*'([^']*)'`)
	licenseRe = regexp.MustCompile(`'license'\s*:\s*'([^']*)'`)
)

// ReadMetadata reads the version and the license FaceFusion states about itself ("3.9.2", "OpenRAIL-AS") from facefusion/metadata.py.
func ReadMetadata(dir string) (version, license string) {
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(MetadataFile))) //nolint:gosec // inside the directory the user chose
	if err != nil {
		return "", ""
	}
	if m := versionRe.FindSubmatch(raw); m != nil {
		version = string(m[1])
	}
	if m := licenseRe.FindSubmatch(raw); m != nil {
		license = string(m[1])
	}
	return version, license
}

// DefaultFinder is the Finder of this machine for a user whose home directory is home ("" leaves the places under it out): the directories a
// `git clone` of the FaceFusion repository usually lands in, and the Python of a conda environment called "facefusion" (the name the
// official installation guide gives it) under Miniconda and Anaconda.
func DefaultFinder(home string) Finder {
	f := Finder{Finder: locate.DefaultFinder()}
	if home == "" {
		return f
	}
	for _, d := range []string{"facefusion", filepath.Join("Documents", "facefusion"), filepath.Join("Desktop", "facefusion")} {
		f.Dirs = append(f.Dirs, filepath.Join(home, d))
	}
	for _, conda := range []string{"miniconda3", "anaconda3", "miniforge3"} {
		env := filepath.Join(home, conda, "envs", "facefusion")
		if f.GOOS == "windows" {
			f.Pythons = append(f.Pythons, filepath.Join(env, "python.exe"))
		} else {
			f.Pythons = append(f.Pythons, filepath.Join(env, "bin", "python"))
		}
	}
	return f
}
