package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

// AppleEngine is the ComfyUI build for Apple silicon (data/engine_apple.json). The project ships a portable package for Windows only, so
// mam puts this one together itself, the way it assembles MuseTalk (spec/architecture/model-manager/model-manager.md §3.7): the
// relocatable CPython of python-build-standalone, the ComfyUI source of the pinned commit, and every Python wheel of ComfyUI's
// requirements.txt at that commit (PyTorch, which has the Metal (MPS) backend, included), each fetched by SHA-256 and extracted by mam.
// Nothing is run: no pip, no setup script.
type AppleEngine struct {
	Label string `json:"label"`
	// Commit is the commit of ComfyUI that Engine.Version tags: the source part is the archive of exactly this commit.
	Commit string `json:"commit"`
	// Python and Main are the interpreter and the script of the launch, relative to the engine directory; SitePackages is where the
	// wheels go.
	Python       string `json:"python"`
	Main         string `json:"main"`
	SitePackages string `json:"site_packages"`
	// MinMacOS is the lowest major version of macOS the pinned wheels run on (the highest macosx tag among them).
	MinMacOS int `json:"min_macos"`
	// Env is added to the environment of the launch ("NAME=value").
	Env []string `json:"env"`
	// Size and ExtractedSize are the sums over Parts (the download and the extracted files).
	Size          int64        `json:"size"`
	ExtractedSize int64        `json:"extracted_size"`
	Parts         []parts.Part `json:"parts"`
}

// Asset is the Apple build as the catalog's other builds are described: the label and the sizes of the whole download.
func (a AppleEngine) Asset() Asset {
	return Asset{Label: a.Label, Size: a.Size, ExtractedSize: a.ExtractedSize}
}

// loadApple reads data/engine_apple.json; a catalog without the file has no Apple build (nil).
func loadApple(fsys fs.FS) (*AppleEngine, error) {
	if _, err := fs.Stat(fsys, "data/engine_apple.json"); errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // no Apple build is a catalog, not an error
	}
	var a AppleEngine
	if err := decode(fsys, "data/engine_apple.json", &a); err != nil {
		return nil, err
	}
	return &a, nil
}

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// (the launch environment's pattern, envPattern, is the runtimes' own: validate_runtime.go)
	// macWheel is the platform tag of a wheel that runs on Apple silicon: macosx_<major>_<minor>_arm64 or universal2.
	macWheel = regexp.MustCompile(`^macosx_([0-9]+)_[0-9]+_(arm64|universal2)$`)
)

// validateApple checks the Apple build: the pins of every part, that the source is the pinned commit, and that every wheel is one that
// runs on the Python of the build on Apple silicon and on the macOS version the catalog states.
func validateApple(e Engine) []error {
	a := e.Apple
	if a == nil {
		return nil
	}
	const where = "engine apple"
	var errs []error
	if a.Label == "" {
		errs = append(errs, fmt.Errorf("%s: label is required", where))
	}
	if !commitPattern.MatchString(a.Commit) {
		errs = append(errs, fmt.Errorf("%s: commit must be 40 lower-case hex digits", where))
	}
	for name, p := range map[string]string{"python": a.Python, "main": a.Main, "site_packages": a.SitePackages} {
		if !relativeSlash(p) {
			errs = append(errs, fmt.Errorf("%s: %s %q must be a relative slash path", where, name, p))
		}
	}
	if a.MinMacOS < 11 {
		errs = append(errs, fmt.Errorf("%s: min_macos %d is not a macOS major version Apple silicon runs", where, a.MinMacOS))
	}
	for _, kv := range a.Env {
		if !envPattern.MatchString(kv) {
			errs = append(errs, fmt.Errorf("%s: env %q is not NAME=value", where, kv))
		}
	}
	size, extracted, partErrs := parts.Check(where, a.Parts)
	errs = append(errs, partErrs...)
	if size != a.Size || extracted != a.ExtractedSize {
		errs = append(errs, fmt.Errorf("%s: size and extracted_size must be the sums of the parts (%d and %d)", where, size, extracted))
	}
	return append(errs, applePartRules(a)...)
}

// applePartRules: one Python archive, one source archive of the pinned commit (below the directory Main is in), and wheels for the rest.
func applePartRules(a *AppleEngine) []error {
	const where = "engine apple"
	var errs []error
	var pythons, sources int
	for _, p := range a.Parts {
		switch p.Kind {
		case parts.KindTarGz:
			pythons++
		case parts.KindZip:
			sources++
			want := "https://github.com/Comfy-Org/ComfyUI/archive/" + a.Commit + ".zip"
			if p.URL != want || p.Strip != "ComfyUI-"+a.Commit || p.Dest == "" || !strings.HasPrefix(a.Main, p.Dest+"/") {
				errs = append(errs, fmt.Errorf("%s: the source part must be %s, stripped of ComfyUI-<commit>, below the directory of main", where, want))
			}
		case parts.KindWheel:
			errs = append(errs, checkMacWheel(a, p)...)
		}
	}
	if pythons != 1 || sources != 1 {
		errs = append(errs, fmt.Errorf("%s: it needs one Python archive and one source archive (have %d and %d)", where, pythons, sources))
	}
	return errs
}

// checkMacWheel refuses a wheel that does not run on Apple silicon with the Python 3.13 of the build, or needs a newer macOS than min_macos.
func checkMacWheel(a *AppleEngine, p parts.Part) []error {
	tags := strings.Split(strings.TrimSuffix(path.Base(p.URL), ".whl"), "-")
	if len(tags) < 5 {
		return []error{fmt.Errorf("engine apple part %s: %q is not a wheel file name", p.Name, path.Base(p.URL))}
	}
	pyTag, abiTag, platTag := tags[len(tags)-3], tags[len(tags)-2], tags[len(tags)-1]
	if !pythonOK(pyTag, abiTag) {
		return []error{fmt.Errorf("engine apple part %s: the wheel (%s-%s) does not run on Python 3.13", p.Name, pyTag, abiTag)}
	}
	if platTag == "any" {
		return nil
	}
	m := macWheel.FindStringSubmatch(platTag)
	if m == nil {
		return []error{fmt.Errorf("engine apple part %s: the wheel platform %s is not Apple silicon", p.Name, platTag)}
	}
	if major, _ := strconv.Atoi(m[1]); major > a.MinMacOS {
		return []error{fmt.Errorf("engine apple part %s: the wheel needs macOS %d, min_macos is %d", p.Name, major, a.MinMacOS)}
	}
	return nil
}

// pythonOK reports whether a wheel with these tags imports on CPython 3.13: pure Python, built for 3.13, or built for the stable ABI of
// 3.13 or earlier.
func pythonOK(pyTag, abiTag string) bool {
	for _, py := range strings.Split(pyTag, ".") {
		if py == "py3" || py == "cp313" && (abiTag == "cp313" || abiTag == "abi3" || abiTag == "none") {
			return true
		}
		if v, ok := strings.CutPrefix(py, "cp3"); ok && abiTag == "abi3" {
			if minor, err := strconv.Atoi(v); err == nil && minor <= 13 {
				return true
			}
		}
	}
	return false
}

func relativeSlash(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.Contains(p, `\`) && !strings.Contains(p, ":") && !slices.Contains(strings.Split(p, "/"), "..")
}
