// Package upstream checks, against the places they are fetched from, the URLs that the model catalog pins with a SHA-256 and a size
// (spec/architecture/model-manager/local-runtime.md §12.10): that each file is still there, still has the pinned size and, where the
// upstream says so without a download, still has the pinned SHA-256. mam verifies a file when it fetches it; this package finds out that a
// mirror disappeared or changed before a user's install fails on it. The command is backend/cmd/upstreamcheck.
package upstream

import (
	"cmp"
	"maps"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// Hosts says which download hosts a pin may come from: the check follows the host list the real download uses (fetch.AllowedURL for the
// weights, the engine and the models; fetch.PackageURL for the packages of a local runtime), so a URL that the install would refuse is
// reported instead of being fetched.
type Hosts int

// The host lists of the pins.
const (
	WeightHosts Hosts = iota
	PackageHosts
)

// Pin is one pinned download of the catalog: where it is, and the SHA-256 and size it must have.
type Pin struct {
	// Label says where the pin is in the catalog ("model flux1-dev: ae.safetensors"), for the report.
	Label  string
	URL    string
	SHA256 string
	Size   int64
	Hosts  Hosts
	// Gated: the file is behind a Hugging Face login (Model.RequiresHFToken); it is checked only with a token.
	Gated bool
}

// Pins lists every pinned download of the catalog (the engine builds, the models' files, the runtimes' packages and parts, and the
// assets of the runtimes), one pin per URL, in a stable order. Two pins with the same URL, SHA-256 and size are one download.
func Pins(c *catalog.Catalog) []Pin {
	var all []Pin
	for _, v := range slices.Sorted(maps.Keys(c.Engine.Variants)) {
		a := c.Engine.Variants[v]
		all = append(all, Pin{Label: "engine " + string(v), URL: a.URL, SHA256: a.SHA256, Size: a.Size, Hosts: WeightHosts})
	}
	if c.Engine.Apple != nil {
		for _, p := range c.Engine.Apple.Parts {
			all = append(all, Pin{Label: "engine apple: " + p.Name, URL: p.URL, SHA256: p.SHA256, Size: p.Size, Hosts: PackageHosts})
		}
	}
	for _, m := range c.Models {
		for _, f := range m.Files {
			all = append(all, Pin{Label: "model " + m.ID + ": " + f.Name, URL: f.URL, SHA256: f.SHA256, Size: f.Size, Hosts: WeightHosts, Gated: m.RequiresHFToken})
		}
	}
	for _, r := range c.Runtimes {
		for _, v := range r.VariantList() {
			rv := r.Variants[v]
			if !rv.Assembled() { // an assembled build has no archive of its own: its Asset only holds the label and the sums
				all = append(all, Pin{Label: "runtime " + r.ID + " " + string(v), URL: rv.URL, SHA256: rv.SHA256, Size: rv.Size, Hosts: PackageHosts})
			}
			for _, p := range rv.Parts {
				all = append(all, Pin{Label: "runtime " + r.ID + " " + string(v) + ": " + p.Name, URL: p.URL, SHA256: p.SHA256, Size: p.Size, Hosts: PackageHosts})
			}
		}
	}
	for _, s := range c.ResourceSets {
		for _, a := range s.Assets {
			all = append(all, Pin{Label: "assets " + s.Runtime + ": " + a.Name, URL: a.URL, SHA256: a.SHA256, Size: a.Size, Hosts: WeightHosts})
		}
	}
	slices.SortStableFunc(all, func(a, b Pin) int {
		return cmp.Or(cmp.Compare(a.URL, b.URL), cmp.Compare(a.SHA256, b.SHA256), cmp.Compare(a.Size, b.Size))
	})
	return slices.CompactFunc(all, func(a, b Pin) bool { return a.URL == b.URL && a.SHA256 == b.SHA256 && a.Size == b.Size })
}
