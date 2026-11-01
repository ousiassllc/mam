package catalog

import (
	"cmp"
	"slices"
)

// CommercialUse says whether a weight or a dependency may be used commercially. Unclear is a value of its own, not a "no": the
// license does not say (or says something that mam could not confirm), and the screen shows it prominently.
type CommercialUse string

// The answers.
const (
	CommercialYes     CommercialUse = "yes"
	CommercialNo      CommercialUse = "no"
	CommercialUnclear CommercialUse = "unclear"
)

// ResourceLicense is the license of a weight or a dependency, as the screen and the docs show it. Notice is the sentence that says what
// to watch for: it is required when the commercial use is not "yes" (a model card that says one thing and a training data set that
// says another is exactly the case for it).
type ResourceLicense struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	URL           string        `json:"url"`
	Summary       string        `json:"summary"`
	CommercialUse CommercialUse `json:"commercial_use"`
	Notice        string        `json:"notice,omitempty"`
}

// ResourceSource says where the original of an asset is: the repository or file page the vendor publishes it at. When mam downloads it
// from a place that is not that repository (a third party's mirror, a Hugging Face copy of a file that lies on Google Drive),
// Note says where the original is and the date (YYYY-MM-DD) its SHA-256 was checked to be the mirror's, like File.SourceNote.
type ResourceSource struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Note string `json:"source_note,omitempty"`
}

// ResourceMember is one file of an archive asset: the entry of the archive, where the file goes below the runtime's environment
// directory, and what the file must be once extracted.
type ResourceMember struct {
	Entry  string `json:"entry"`
	Dest   string `json:"dest"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Asset formats of an archive asset (the formats of package exeinstall).
const (
	FormatZip   = "zip"
	FormatTarGz = "tar.gz"
)

// Resource is a weight or a dependency of a runtime that the runtime needs on disk, pinned: a file (Dest) or the files of an archive
// (Format and Members). URL is a Hugging Face file at a commit; SHA256 and Size are those of the downloaded file (the archive for an
// archive asset). Dest is relative to the runtime's environment directory and slash separated. Source and License (an ID of the
// ResourceSet's licenses) are what the screen shows next to it. Required: the runtime is not started without it.
type Resource struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Dest is where a single file goes ("" for an archive).
	Dest string `json:"dest,omitempty"`
	// Format and Members describe an archive ("" and none for a single file).
	Format  string           `json:"format,omitempty"`
	Members []ResourceMember `json:"members,omitempty"`
	URL     string           `json:"url"`
	SHA256  string           `json:"sha256"`
	Size    int64            `json:"size"`
	// Required: the runtime needs the asset to start.
	Required bool           `json:"required"`
	Source   ResourceSource `json:"source"`
	License  string         `json:"license"`
}

// Files lists where the resource's files go (relative to the environment directory) with the size and SHA-256 each must have.
func (a Resource) Files() []ResourceMember {
	if a.Dest != "" {
		return []ResourceMember{{Dest: a.Dest, Size: a.Size, SHA256: a.SHA256}}
	}
	return a.Members
}

// IsArchive reports whether the asset is an archive that holds Members.
func (a Resource) IsArchive() bool { return a.Dest == "" }

// ResourceSet is the weights and dependencies of one runtime (data/assets/<runtime id>.json). It is a file of its own so that a
// runtime's weights are pinned and reviewed apart from the runtime package, and a runtime whose package is not decided yet
// (MuseTalk) can have its weights recorded first.
type ResourceSet struct {
	Runtime  string            `json:"runtime"`
	Licenses []ResourceLicense `json:"licenses"`
	Assets   []Resource        `json:"assets"`
}

// License returns the license with the id.
func (s ResourceSet) License(id string) (ResourceLicense, bool) {
	i := slices.IndexFunc(s.Licenses, func(l ResourceLicense) bool { return l.ID == id })
	if i < 0 {
		return ResourceLicense{}, false
	}
	return s.Licenses[i], true
}

// TotalSize is the size of everything to download, in bytes (an archive that holds several assets counts once).
func (s ResourceSet) TotalSize() int64 {
	var n int64
	seen := map[string]bool{}
	for _, a := range s.Assets {
		if !seen[a.URL] { // two assets can come out of one archive
			n += a.Size
		}
		seen[a.URL] = true
	}
	return n
}

// Unclear lists the assets whose commercial use is not "yes", by name.
func (s ResourceSet) Unclear() []string {
	var names []string
	for _, a := range s.Assets {
		if l, ok := s.License(a.License); ok && l.CommercialUse != CommercialYes {
			names = append(names, a.Name)
		}
	}
	return names
}

// ResourceSet returns the weights and dependencies of the runtime (false for a runtime without any).
func (c *Catalog) ResourceSet(runtimeID string) (ResourceSet, bool) {
	i := slices.IndexFunc(c.ResourceSets, func(s ResourceSet) bool { return s.Runtime == runtimeID })
	if i < 0 {
		return ResourceSet{}, false
	}
	return c.ResourceSets[i], true
}

func sortResourceSets(sets []ResourceSet) {
	slices.SortFunc(sets, func(a, b ResourceSet) int { return cmp.Compare(a.Runtime, b.Runtime) })
}
