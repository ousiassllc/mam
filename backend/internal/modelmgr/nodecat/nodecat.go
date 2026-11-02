// Package nodecat is the allowlist of the custom nodes the bundled ComfyUI may load (spec/architecture/model-manager/custom-nodes.md).
// The engine starts with --disable-all-custom-nodes, so a folder in custom_nodes does nothing by itself; the only folders it is told to
// load (--whitelist-custom-nodes) are the ones of the nodes below that mam installed from the pinned archive and that still match it.
// It is data embedded in the binary (data/nodes/*.json, data/python.json): a new node is a new JSON file and a new mam release,
// and the tests (nodecat_test.go) refuse an entry whose pin, license, dependencies or review are not stated.
package nodecat

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

//go:embed data
var dataFS embed.FS

// Node is a custom node of ComfyUI that mam may install: one pinned commit of a GitHub repository.
//
// To add one (#960 PuLID, #985 PoseStudio, ...): put the pinned archive's URL, SHA-256 and sizes here, copy the license from the
// repository's LICENSE (not a summary), name what the node needs from Python, read its source for what it does (network, subprocess,
// exec, unsafe loads) and say so in Review. The validation fails while any of it is missing. A node that needs a Python package the
// bundled environment does not have stays in the list with InstallBlocked: it is shown with the reason and never installed.
type Node struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Repo is the GitHub repository (https://github.com/<owner>/<repo>); Commit the 40-hex commit the archive is of. Never a branch or tag.
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	// ArchiveURL is Repo + "/archive/" + Commit + ".zip"; the install fetches it and checks ArchiveSHA256 and ArchiveSize, and refuses an
	// archive that holds more than ExtractedSize bytes.
	ArchiveURL    string `json:"archive_url"`
	ArchiveSHA256 string `json:"archive_sha256"`
	ArchiveSize   int64  `json:"archive_size"`
	ExtractedSize int64  `json:"extracted_size"`
	// Folder is the directory below custom_nodes the archive is extracted to: the name given to --whitelist-custom-nodes.
	Folder string `json:"folder"`
	// License is the license of the node's code, as the repository's LICENSE states it; Commercial is the answer for using it.
	License catalog.License `json:"license"`
	// PythonRequires are the Python packages the node imports when it loads (the import names of the distribution: "torch", "einops").
	// Each has to be in python.json (what the bundled environment has) unless InstallBlocked is set. PythonOptional are imported only
	// by some of its nodes when they run (IPAdapter's FaceID nodes import insightface): the rest keeps working, those nodes fail.
	PythonRequires []string `json:"python_requires"`
	PythonOptional []string `json:"python_optional,omitempty"`
	// InstallBlocked, when set, is why mam does not install the node (a package it needs is not in the bundled environment, and mam
	// runs no pip). The entry is shown with the reason; no install, no whitelist.
	InstallBlocked string `json:"install_blocked,omitempty"`
	// NodeClasses are the node classes the node adds that mam relies on: /object_info lists them once the engine loaded the node.
	NodeClasses []string `json:"node_classes"`
	// ModelFolders are the model folders the node registers (folder_paths) besides the core ones, such as "ipadapter": a model with a file
	// there needs the node. The loader class of such a folder goes into catalog's loaderInputs, with the folder in its modelFolders.
	ModelFolders []string `json:"model_folders,omitempty"`
	// Limits says what of the node does not work in the bundled environment (the nodes that need a package that is not there).
	Limits string `json:"limits,omitempty"`
	// ReviewedAt is the date (YYYY-MM-DD) the source of Commit was read; Review is what was found: network access, subprocess, exec and
	// eval, unsafe loads of pickle files, files it writes. A custom node is arbitrary code in the engine's process, so this is the record.
	ReviewedAt string `json:"reviewed_at"`
	Review     string `json:"review"`
}

// Installable reports whether mam installs the node.
func (n Node) Installable() bool { return n.InstallBlocked == "" }

// Python is what the bundled environment is known to have: the packages the pinned ComfyUI release requires (its requirements.txt).
// Nothing else is assumed (mam runs no pip), so a node that imports anything else is blocked.
type Python struct {
	// ComfyUI is the engine release the list is read from; it has to be the pinned one, so a new engine makes the list be read again.
	ComfyUI string `json:"comfyui"`
	// Source says where the list comes from.
	Source string `json:"source"`
	// Packages are the import names, lower case.
	Packages []string `json:"packages"`
}

// Has reports whether the environment has the package (an import name, compared without case).
func (p Python) Has(pkg string) bool {
	return slices.Contains(p.Packages, strings.ToLower(pkg))
}

// Catalog is the allowlist.
type Catalog struct {
	Nodes  []Node
	Python Python
}

// Node returns the node with the ID.
func (c *Catalog) Node(id string) (Node, bool) {
	i := slices.IndexFunc(c.Nodes, func(n Node) bool { return n.ID == id })
	if i < 0 {
		return Node{}, false
	}
	return c.Nodes[i], true
}

// ByFolder returns the node installed in the folder of custom_nodes.
func (c *Catalog) ByFolder(folder string) (Node, bool) {
	i := slices.IndexFunc(c.Nodes, func(n Node) bool { return n.Folder == folder })
	if i < 0 {
		return Node{}, false
	}
	return c.Nodes[i], true
}

var (
	once    sync.Once
	loaded  *Catalog
	loadErr error
)

// Default is the embedded allowlist. It panics when the embedded data is invalid: a build problem the tests catch.
func Default() *Catalog {
	once.Do(func() { loaded, loadErr = Load(dataFS) })
	if loadErr != nil {
		panic(fmt.Sprintf("custom node allowlist: %v", loadErr))
	}
	return loaded
}

// Load reads data/python.json and data/nodes/*.json of fsys, refusing unknown fields. The nodes are in the order of their ID.
func Load(fsys fs.FS) (*Catalog, error) {
	var c Catalog
	if err := decode(fsys, "data/python.json", &c.Python); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(fsys, "data/nodes")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		var n Node
		if err := decode(fsys, "data/nodes/"+e.Name(), &n); err != nil {
			return nil, err
		}
		c.Nodes = append(c.Nodes, n)
	}
	slices.SortFunc(c.Nodes, func(a, b Node) int { return cmp.Compare(a.ID, b.ID) })
	return &c, nil
}

func decode(fsys fs.FS, name string, into any) error {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
