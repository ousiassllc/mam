// Package catalog is the pinned Codex app-server of the "Codex 経由" ChatGPT login (spec/architecture/chatgpt-signin.md
// §8): one released version of openai/codex, and for each supported platform (Windows x64 / arm64, macOS arm64) the
// archive asset to download (a zip, or a tar.gz for macOS) with its SHA-256 and size and the executable inside it with
// its SHA-256 and size. It is data embedded in the binary, like the
// ComfyUI catalog: mam ships none of Codex's bytes, a download is checked against what was reviewed when the catalog
// was written, and a new Codex version is a new mam release (the JSON-RPC schema changes with the version, so mam uses
// only a version it was checked against; protocol's schema test compares this version with the schema it uses).
package catalog

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
)

//go:embed data/catalog.json
var data []byte

// License is the license of Codex, shown with the download.
type License struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// The archive formats of an Asset.
const (
	FormatZip   = "zip"
	FormatTarGz = "tar.gz"
)

// Asset is the Codex app-server of one platform: the archive to download and the executable it holds.
type Asset struct {
	URL string `json:"url"`
	// Format is the archive format: FormatZip (Windows) or FormatTarGz (macOS). The URL ends with it.
	Format string `json:"format"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// Exe is the name of the single file of the archive; ExeSHA256 and ExeSize are what it must be after extraction.
	Exe       string `json:"exe"`
	ExeSHA256 string `json:"exe_sha256"`
	ExeSize   int64  `json:"exe_size"`
}

// Catalog is the pinned version of Codex.
type Catalog struct {
	// Version is the Codex version (the release rust-v<Version>).
	Version   string           `json:"version"`
	Tag       string           `json:"tag"`
	SourceURL string           `json:"source_url"`
	License   License          `json:"license"`
	Assets    map[string]Asset `json:"assets"` // by GOARCH
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
		panic(fmt.Sprintf("codex catalog: %v", loadErr))
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

// Key is the key of Catalog.Assets: "<GOOS>/<GOARCH>", e.g. "windows/amd64" or "darwin/arm64".
func Key(goos, goarch string) string { return goos + "/" + goarch }

// AssetFor is the asset of a platform; ok is false for a platform with no pinned build (Linux, macOS on Intel, ...).
func (c Catalog) AssetFor(goos, goarch string) (Asset, bool) {
	a, ok := c.Assets[Key(goos, goarch)]
	return a, ok
}

// platforms are the platforms a catalog may pin an asset for.
var platforms = map[string]bool{"windows/amd64": true, "windows/arm64": true, "darwin/arm64": true}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate checks that every asset is of a known platform and that every download is an https URL of a release of tag
// on github.com whose name ends with the archive format, with a SHA-256 and a size.
func (c Catalog) Validate() error {
	var errs []error
	if c.Version == "" || c.Tag != "rust-v"+c.Version {
		errs = append(errs, fmt.Errorf("tag %q is not rust-v%s", c.Tag, c.Version))
	}
	if c.License.ID == "" || c.License.URL == "" {
		errs = append(errs, errors.New("license is empty"))
	}
	if len(c.Assets) == 0 {
		errs = append(errs, errors.New("no asset"))
	}
	for key, a := range c.Assets {
		errs = append(errs, c.validateAsset(key, a)...)
	}
	return errors.Join(errs...)
}

// validateAsset checks one asset: the platform key, the download URL and its format, the checksums, the sizes and the
// name of the executable.
func (c Catalog) validateAsset(key string, a Asset) []error {
	var errs []error
	if !platforms[key] {
		return []error{fmt.Errorf("%s: not a platform with a Codex build (%s)", key, strings.Join(slices.Sorted(maps.Keys(platforms)), ", "))}
	}
	prefix := "/openai/codex/releases/download/" + c.Tag + "/"
	u, err := url.Parse(a.URL)
	switch {
	case err != nil || u.Scheme != "https" || u.Host != "github.com" || !strings.HasPrefix(u.Path, prefix):
		errs = append(errs, fmt.Errorf("%s: url %q is not a release asset of %s", key, a.URL, c.Tag))
	case u.RawQuery != "" || u.Fragment != "":
		errs = append(errs, fmt.Errorf("%s: url has a query or fragment", key))
	}
	if (a.Format != FormatZip && a.Format != FormatTarGz) || !strings.HasSuffix(a.URL, "."+a.Format) {
		errs = append(errs, fmt.Errorf("%s: format %q must be %s or %s and end the url", key, a.Format, FormatZip, FormatTarGz))
	}
	if !sha256Hex.MatchString(a.SHA256) || !sha256Hex.MatchString(a.ExeSHA256) {
		errs = append(errs, fmt.Errorf("%s: sha256 must be 64 lower-case hex digits", key))
	}
	if a.Size <= 0 || a.ExeSize <= 0 {
		errs = append(errs, fmt.Errorf("%s: size must be positive", key))
	}
	// A Windows executable is a .exe; a macOS one has no extension. Either way a bare file name.
	if a.Exe == "" || strings.ContainsAny(a.Exe, `/\`) || a.Exe == "." || a.Exe == ".." || strings.HasSuffix(a.Exe, ".exe") != strings.HasPrefix(key, "windows/") {
		errs = append(errs, fmt.Errorf("%s: exe %q must be a bare file name (.exe on Windows only)", key, a.Exe))
	}
	return errs
}
