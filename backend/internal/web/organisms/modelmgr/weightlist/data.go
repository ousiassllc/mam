// Package weightlist is the list of the weights and dependencies of a local runtime, each with where it comes from and its license and
// whether it may be used commercially (the card of a runtime on the model manager screen; it is a package of its own because that
// package is at its size limit).
package weightlist

// Commercial-use values of a license (catalog.CommercialUse as the screen uses it).
const (
	Yes     = "yes"
	No      = "no"
	Unclear = "unclear"
)

// License is the license of a weight or a dependency, as the card shows it. Commercial is one of the Yes / No / Unclear values;
// Notice is what to watch for (always set when it is not "yes": the catalog requires it).
type License struct {
	Name       string
	URL        string
	Summary    string
	Commercial string
	Notice     string
}

// Asset is one weight or dependency of a runtime: what it is, whether it is there, where it comes from and its license.
type Asset struct {
	Name        string
	Description string
	Size        string
	// State is ok, missing or unverified (runtime.AssetState).
	State    string
	Required bool
	// SourceName and SourceURL are where the original is; SourceNote says where the original is when mam fetches a copy.
	SourceName string
	SourceURL  string
	SourceNote string
	License    License
}

// Weights is the weights and dependencies of a runtime.
type Weights struct {
	// State is none (the package is not installed), ready, missing or unverified.
	State   string
	Message string
	Assets  []Asset
	// NotReady counts the required assets that are not verified; Need is the free space the download of the missing ones needs.
	NotReady int
	Need     string
	Total    string
	// Unclear are the names of the assets whose commercial use is not "yes" (the card says so before the list).
	Unclear []string
}
