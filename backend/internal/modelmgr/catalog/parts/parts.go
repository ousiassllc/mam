// Package parts describes the pinned parts of a local runtime that mam puts together itself (MuseTalk: the vendor ships no package, so
// the embeddable Python, the vendor's source at a commit and each Python wheel are fetched by SHA-256 and extracted by mam), and checks
// them. The catalog embeds the types in its runtime (catalog.RuntimeVariant.Parts, catalog.Runtime.Build) and calls Check and CheckBuild
// from its own validation (spec/architecture/model-manager/local-runtime.md §13.1).
package parts

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The kinds of a Part.
const (
	// KindZip is a zip archive extracted into a directory below the environment (Dest), without its single top directory (Strip).
	KindZip = "zip"
	// KindWheel is a Python wheel (a zip with the layout of a package), put into the site-packages directory of Build.
	KindWheel = "wheel"
)

// Part is one pinned file of a package that mam puts together itself. Every part has its SHA-256 and size, and its extraction writes
// nothing outside the environment and not more than ExtractedSize.
type Part struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// ExtractedSize is the size of the files once extracted (measured on the real archive); the extraction stops at it.
	ExtractedSize int64 `json:"extracted_size"`
	// Dest is the directory of a KindZip below the environment ("" for the environment itself), Strip the single top directory inside
	// it whose content is extracted ("" for none). A KindWheel has neither.
	Dest  string `json:"dest,omitempty"`
	Strip string `json:"strip,omitempty"`
}

// File is a file that mam writes into the environment after the parts are extracted: the embedded Overlay (a file of
// catalog/data/overlays), or Lines (a text file, one line each).
type File struct {
	Dest    string   `json:"dest"`
	Overlay string   `json:"overlay,omitempty"`
	Lines   []string `json:"lines,omitempty"`
}

// Build says how the parts become an environment: where the wheels go, and the files written at the end (the path file of the
// embeddable Python, mam's own server scripts).
type Build struct {
	// SitePackages is the directory (relative to the environment) the wheels are extracted into.
	SitePackages string `json:"site_packages"`
	Files        []File `json:"files"`
}

// A part comes from one of these places only (fetch.PackageURL lets the same hosts through). Every URL is pinned by its SHA-256, so
// the host is where the bytes are fetched from and is not what they are trusted by.
var urlPatterns = []*regexp.Regexp{
	// The embeddable Python of python.org.
	regexp.MustCompile(`^https://www\.python\.org/ftp/python/[0-9]+\.[0-9]+\.[0-9]+/python-[0-9]+\.[0-9]+\.[0-9]+-embed-amd64\.zip$`),
	// A wheel of PyPI (files.pythonhosted.org/packages/<hash path>/<file>).
	regexp.MustCompile(`^https://files\.pythonhosted\.org/packages/[0-9a-f/]+/[A-Za-z0-9][A-Za-z0-9._+-]*\.whl$`),
	// PyTorch's wheels of one CUDA build, and OpenMMLab's wheels of the mmcv build for that PyTorch.
	regexp.MustCompile(`^https://download\.pytorch\.org/whl/cu[0-9]+/[A-Za-z0-9][A-Za-z0-9._%+-]*\.whl$`),
	regexp.MustCompile(`^https://download\.openmmlab\.com/mmcv/dist/cu[0-9]+/torch[0-9.]+/[A-Za-z0-9][A-Za-z0-9._-]*\.whl$`),
	// The vendor's source at a commit of a GitHub repository.
	regexp.MustCompile(`^https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/archive/[0-9a-f]{40}\.zip$`),
}

// URLAllowed reports whether url is a place a part may come from.
func URLAllowed(url string) bool {
	return slices.ContainsFunc(urlPatterns, func(p *regexp.Regexp) bool { return p.MatchString(url) })
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// relativeSlashPath reports whether p is a non-empty relative path with "/" separators that stays below its directory.
func relativeSlashPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.Contains(p, `\`) && !strings.Contains(p, ":") &&
		!slices.Contains(strings.Split(p, "/"), "..")
}

// Check validates the parts of the build named where and returns the sums of their sizes and extracted sizes (the variant of the
// catalog must state the same).
func Check(where string, ps []Part) (size, extracted int64, errs []error) {
	seen := map[string]bool{}
	for _, p := range ps {
		pw := where + " part " + p.Name
		switch {
		case p.Name == "" || seen[p.Name]:
			errs = append(errs, fmt.Errorf("%s: a part needs a name that is used once", pw))
		case !URLAllowed(p.URL):
			errs = append(errs, fmt.Errorf("%s: url %q is not one of the places a part may come from", pw, p.URL))
		case !sha256Pattern.MatchString(p.SHA256):
			errs = append(errs, fmt.Errorf("%s: sha256 must be 64 lower-case hex digits", pw))
		case p.Size <= 0 || p.ExtractedSize <= 0:
			errs = append(errs, fmt.Errorf("%s: size and extracted_size are required", pw))
		}
		seen[p.Name] = true
		size += p.Size
		extracted += p.ExtractedSize
		errs = append(errs, checkKind(pw, p)...)
	}
	return size, extracted, errs
}

func checkKind(pw string, p Part) []error {
	switch p.Kind {
	case KindZip:
		if (p.Dest != "" && !relativeSlashPath(p.Dest)) || (p.Strip != "" && !relativeSlashPath(p.Strip)) {
			return []error{fmt.Errorf("%s: dest and strip must be relative slash paths", pw)}
		}
	case KindWheel:
		if p.Dest != "" || p.Strip != "" || !strings.HasSuffix(p.URL, ".whl") {
			return []error{fmt.Errorf("%s: a wheel has a .whl url and no dest or strip", pw)}
		}
	default:
		return []error{fmt.Errorf("%s: kind must be zip or wheel", pw)}
	}
	return nil
}

// CheckBuild validates the build of the runtime named where: the wheels go to a place inside the environment, and each file written at
// the end is inside the environment and is either an embedded script (one of overlays) or lines.
func CheckBuild(where string, b *Build, overlays map[string][]byte) []error {
	var errs []error
	if !relativeSlashPath(b.SitePackages) {
		errs = append(errs, fmt.Errorf("%s: build site_packages must be a relative slash path", where))
	}
	for _, f := range b.Files {
		_, haveOverlay := overlays[f.Overlay]
		switch {
		case !relativeSlashPath(f.Dest):
			errs = append(errs, fmt.Errorf("%s: build file dest %q must be a relative slash path", where, f.Dest))
		case (f.Overlay == "") == (len(f.Lines) == 0):
			errs = append(errs, fmt.Errorf("%s: build file %s has an overlay or lines, one of them", where, f.Dest))
		case f.Overlay != "" && !haveOverlay:
			errs = append(errs, fmt.Errorf("%s: build file %s: there is no overlay %q", where, f.Dest, f.Overlay))
		}
	}
	return errs
}
