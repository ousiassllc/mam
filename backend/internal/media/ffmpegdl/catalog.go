// Package ffmpegdl is the ffmpeg and ffprobe that mam downloads for the user on Windows (x64) and macOS (Apple silicon)
// when they are on the machine nowhere else (spec/architecture/ffmpeg-download.md): the pinned build of each platform with
// the SHA-256 of every archive and of every executable in it, the download and the install into the data directory, and
// where the installed copy lies, which the search of media/locate puts last so that an ffmpeg the user installed always wins.
// mam ships none of ffmpeg's bytes. The catalog is data embedded in the binary, like the Codex and ComfyUI catalogs: a download
// is checked against what was reviewed when the catalog was written, a new ffmpeg is a new mam release, and nothing is
// fetched but by a click of the user.
package ffmpegdl

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/exeinstall"
	"github.com/ousiassllc/mam/backend/internal/media/locate"
)

//go:embed data/catalog.json
var data []byte

// DownloadHosts are the hosts (and their subdomains) a pinned URL may be on: GitHub releases (the Windows build) and the build
// server of the macOS build. The download follows redirects within them only.
var DownloadHosts = []string{"github.com", "githubusercontent.com", "ffmpeg.martin-riedl.de"}

// License is the license of the build, shown with the download.
type License struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Member is one of the two executables inside an archive: where it lies in the archive, the name it is installed under, and
// what it must be after the extraction.
type Member struct {
	Entry  string `json:"entry"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Archive is one file to download and the executables to take out of it.
type Archive struct {
	URL     string   `json:"url"`
	Format  string   `json:"format"`
	SHA256  string   `json:"sha256"`
	Size    int64    `json:"size"`
	Members []Member `json:"members"`
}

// Platform is the pinned build of one platform. Together the Members of its Archives are exactly ffmpeg and ffprobe.
type Platform struct {
	// Version is the ffmpeg version of the build ("9.0.2"). Build, Publisher and SourceURL say where it comes from;
	// SourceCodeURL is the source of that very build.
	Version       string    `json:"version"`
	Build         string    `json:"build"`
	Publisher     string    `json:"publisher"`
	SourceURL     string    `json:"source_url"`
	SourceCodeURL string    `json:"source_code_url"`
	License       License   `json:"license"`
	Archives      []Archive `json:"archives"`
}

// Catalog is the pinned builds, by "<GOOS>/<GOARCH>".
type Catalog struct {
	Platforms map[string]Platform `json:"platforms"`
}

var (
	once    sync.Once
	loaded  Catalog
	loadErr error
)

// Default is the embedded catalog. It panics when the embedded data is invalid: a build problem that the tests catch.
func Default() Catalog {
	once.Do(func() { loaded, loadErr = Parse(data) })
	if loadErr != nil {
		panic(fmt.Sprintf("ffmpeg catalog: %v", loadErr))
	}
	return loaded
}

// Parse reads and validates a catalog, refusing unknown fields.
func Parse(b []byte) (Catalog, error) {
	var c Catalog
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Catalog{}, err
	}
	return c, c.Validate()
}

// Key is the key of Catalog.Platforms: "<GOOS>/<GOARCH>", e.g. "windows/amd64" or "darwin/arm64".
func Key(goos, goarch string) string { return goos + "/" + goarch }

// For is the build of a platform; ok is false for a platform with no pinned build (Linux, Windows on arm64, macOS on Intel).
func (c Catalog) For(goos, goarch string) (Platform, bool) {
	p, ok := c.Platforms[Key(goos, goarch)]
	return p, ok
}

// executableNames are the file names the two executables are installed under on goos.
func executableNames(goos string) []string {
	if goos == "windows" {
		return []string{locate.FFmpeg + ".exe", locate.FFprobe + ".exe"}
	}
	return []string{locate.FFmpeg, locate.FFprobe}
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate checks every platform: a known one, an https URL on a download host that names one release (not "latest"), a
// SHA-256 and a size for every archive and executable, and the installed names being exactly ffmpeg and ffprobe.
func (c Catalog) Validate() error {
	if len(c.Platforms) == 0 {
		return errors.New("no platform")
	}
	var errs []error
	for key, p := range c.Platforms {
		errs = append(errs, p.validate(key)...)
	}
	return errors.Join(errs...)
}

func (p Platform) validate(key string) []error {
	goos, _, _ := strings.Cut(key, "/")
	if key != Key("windows", "amd64") && key != Key("darwin", "arm64") {
		return []error{fmt.Errorf("%s: not a platform ffmpeg is downloaded for", key)}
	}
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(key+": "+format, args...)) }
	for name, v := range map[string]string{"version": p.Version, "build": p.Build, "publisher": p.Publisher, "license id": p.License.ID, "license name": p.License.Name} {
		if v == "" {
			fail("%s is empty", name)
		}
	}
	for name, v := range map[string]string{"source_url": p.SourceURL, "source_code_url": p.SourceCodeURL, "license url": p.License.URL} {
		if u, err := url.Parse(v); err != nil || u.Scheme != "https" || u.Hostname() == "" {
			fail("%s %q is not an https URL", name, v)
		}
	}
	var names []string
	files := map[string]bool{}
	for _, a := range p.Archives {
		errs = append(errs, a.validate(key, files)...)
		for _, m := range a.Members {
			names = append(names, m.Name)
		}
	}
	if len(p.Archives) == 0 {
		fail("no archive")
	}
	want := executableNames(goos)
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		fail("the members are %v, want %v", names, want)
	}
	return errs
}

func (a Archive) validate(key string, files map[string]bool) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(key+" "+path.Base(a.URL)+": "+format, args...))
	}
	u, err := url.Parse(a.URL)
	switch {
	case err != nil || u.Scheme != "https" || u.Hostname() == "":
		fail("the URL %q is not an https URL", a.URL)
	case u.RawQuery != "" || u.Fragment != "":
		fail("the URL has a query or fragment")
	case !hostAllowed(u.Hostname()):
		fail("the host %q is not a download host", u.Hostname())
	case strings.Contains(strings.ToLower(u.Path), "latest"):
		fail("the URL names no fixed release (\"latest\")")
	}
	if base := path.Base(a.URL); files[base] {
		fail("two archives are called %s", base)
	} else {
		files[base] = true
	}
	if a.Format != exeinstall.FormatZip && a.Format != exeinstall.FormatTarGz {
		fail("format %q", a.Format)
	} else if !strings.HasSuffix(a.URL, "."+a.Format) {
		fail("the URL does not end with .%s", a.Format)
	}
	if !sha256Hex.MatchString(a.SHA256) || a.Size <= 0 {
		fail("sha256 or size is not valid")
	}
	if len(a.Members) == 0 {
		fail("no member")
	}
	for _, m := range a.Members {
		switch {
		case m.Entry == "" || strings.HasPrefix(m.Entry, "/") || strings.Contains(m.Entry, ".."):
			fail("the entry %q", m.Entry)
		case m.Name == "" || m.Name != path.Base(m.Name) || strings.ContainsAny(m.Name, `\:`):
			fail("the name %q is not a bare file name", m.Name)
		case !sha256Hex.MatchString(m.SHA256) || m.Size <= 0:
			fail("%s: sha256 or size is not valid", m.Name)
		}
	}
	return errs
}

func hostAllowed(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, d := range DownloadHosts {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// DownloadBytes is the size of all the archives of the platform.
func (p Platform) DownloadBytes() int64 {
	var n int64
	for _, a := range p.Archives {
		n += a.Size
	}
	return n
}

// ExtractedBytes is the size of the two executables.
func (p Platform) ExtractedBytes() int64 {
	var n int64
	for _, a := range p.Archives {
		for _, m := range a.Members {
			n += m.Size
		}
	}
	return n
}

// Members are the executables of all the archives, in order.
func (p Platform) Members() []Member {
	var out []Member
	for _, a := range p.Archives {
		out = append(out, a.Members...)
	}
	return out
}
