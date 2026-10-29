// Package catalog is the list of the local models the model manager can install, and the pinned ComfyUI
// engine it downloads (spec/architecture/model-manager/model-manager.md §3.1, §4). It is data embedded in the binary
// (data/engine.json, data/models/*.json): every URL is pinned to a commit or a release tag and every file has
// its SHA-256 and size, so a download is checked against what was reviewed when the catalog was written
// (the idea of scripts/fetch-face-models.sh). A new model or a new engine version is a new mam release.
package catalog

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

//go:embed data
var dataFS embed.FS

// Variant is the build of the ComfyUI portable package: the GPU it is made for.
type Variant string

// The portable builds of ComfyUI.
const (
	VariantNVIDIA      Variant = "nvidia"       // RTX 20 series and later (CUDA 13)
	VariantNVIDIACU126 Variant = "nvidia_cu126" // GTX 10 series and older (CUDA 12.6)
	VariantAMD         Variant = "amd"
	VariantIntel       Variant = "intel"
)

// Tier says how a model is offered: Standard models are listed first, Advanced ones are the models that need
// a license the user has to accept, or whose license is restricted.
type Tier string

// The tiers of the model list.
const (
	TierStandard Tier = "standard"
	TierAdvanced Tier = "advanced"
)

// License is the license of model weights (or of the engine) as it is shown before an installation. ID and
// Version identify what the user accepts: a new Version asks for a new acceptance.
type License struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
	// CommercialUse says commercial use is allowed (CommercialCondition says on what condition, if any).
	CommercialUse       bool   `json:"commercial_use"`
	CommercialCondition string `json:"commercial_condition,omitempty"`
	// ExcludedRegions are the regions (EU, GB, KR) where the license does not apply: the model must not be
	// used there.
	ExcludedRegions []string `json:"excluded_regions,omitempty"`
	// Notice is the sentence the consent text ends with.
	Notice string `json:"notice,omitempty"`
}

// File is one file of a model, pinned. Folder is the ComfyUI model folder it goes to (checkpoints,
// diffusion_models, text_encoders, vae, loras, latent_upscale_models), Name its file name there. A file that two models share (the same
// Folder and Name) has the same SHA256 and Size in both. SourceNote is set for a file that a third party mirrors
// (the original lies elsewhere, such as Civitai): where the original is and the date its SHA-256 was checked to be
// the mirror's. It is shown before the install, and a mirror that is gone is reported as such.
type File struct {
	Name       string `json:"name"`
	Folder     string `json:"folder"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	SourceNote string `json:"source_note,omitempty"`
}

// Path is where the file lies below the base directory of the engine: models/<folder>/<name>.
func (f File) Path() string { return path.Join("models", f.Folder, f.Name) }

// VRAM is the guideline of the video memory a model needs, in GB. It is a 目安: Source says where it comes
// from, and Estimate is true when it is mam's own conservative figure with no source (not run on a GPU).
type VRAM struct {
	MinGB    int    `json:"min_gb"`
	Source   string `json:"source"`
	Estimate bool   `json:"estimate"`
}

// Workflow is a #831 model created on the bundled endpoint when the model is installed: Name is its model
// name (the identifier is comfyui:bundled-comfyui/<Name>), Template the localcfg workflow template it starts from.
type Workflow struct {
	Name     string `json:"name"`
	Template string `json:"template"`
}

// ContentRating says who a model is for. An adult model (an uncensored model, or a LoRA that adds that) is hidden
// from the list, the install and the trial generation until the age confirmation is on record
// (modelmanager.Service.SetAdultContent); the catalog rule is that it is an advanced model (consent required).
type ContentRating string

// The content ratings.
const (
	RatingGeneral ContentRating = "general"
	RatingAdult   ContentRating = "adult"
)

// Model is a model of the catalog.
type Model struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Kind          localcfg.Media `json:"kind"`
	Tier          Tier           `json:"tier"`
	ContentRating ContentRating  `json:"content_rating"`
	Description   string         `json:"description"`
	// TriggerWords are the words a LoRA of the model needs in the prompt (none for a model without a LoRA).
	TriggerWords []string `json:"trigger_words,omitempty"`
	Files        []File   `json:"files"`
	VRAM         VRAM     `json:"vram"`
	License      License  `json:"license"`
	// ConsentRequired: the license has to be accepted (recorded with its ID and version) before the install.
	ConsentRequired bool `json:"consent_required"`
	// RequiresHFToken says the files are behind a Hugging Face login (a gated repository such as Lightricks/LTX-2.5): the install
	// refuses without a stored token and checks the access before it makes a job.
	RequiresHFToken bool       `json:"requires_hf_token"`
	Workflows       []Workflow `json:"workflows"`
}

// Adult reports whether the model is for adults only.
func (m Model) Adult() bool { return m.ContentRating == RatingAdult }

// TotalSize is the size of all the files of the model in bytes.
func (m Model) TotalSize() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// Asset is a pinned archive of the engine: one portable build.
type Asset struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	// ExtractedSize is the size of the files once extracted (measured on the archive's header).
	ExtractedSize int64 `json:"extracted_size"`
}

// Engine is the pinned ComfyUI release. Version changes with the catalog: installing the engine again with a
// newer pinned version is the engine update.
type Engine struct {
	Version   string `json:"version"`
	SourceURL string `json:"source_url"`
	// ArchiveRoot is the single directory inside the archives; its content is the engine.
	ArchiveRoot string `json:"archive_root"`
	// Python and Main are the interpreter and the script of the launch, relative to the engine directory.
	Python   string            `json:"python"`
	Main     string            `json:"main"`
	License  License           `json:"license"`
	Variants map[Variant]Asset `json:"variants"`
}

// Catalog is the engine and the models.
type Catalog struct {
	Engine Engine
	Models []Model
}

// Model returns the model with the ID.
func (c *Catalog) Model(id string) (Model, bool) {
	i := slices.IndexFunc(c.Models, func(m Model) bool { return m.ID == id })
	if i < 0 {
		return Model{}, false
	}
	return c.Models[i], true
}

// VariantList lists the engine builds in a fixed order.
func (Engine) VariantList() []Variant {
	return []Variant{VariantNVIDIA, VariantNVIDIACU126, VariantAMD, VariantIntel}
}

var (
	once    sync.Once
	loaded  *Catalog
	loadErr error
)

// Default is the embedded catalog. It panics when the embedded data is invalid: a build problem that the
// package's tests catch.
func Default() *Catalog {
	once.Do(func() { loaded, loadErr = Load(dataFS) })
	if loadErr != nil {
		panic(fmt.Sprintf("model catalog: %v", loadErr))
	}
	return loaded
}

// Load reads data/engine.json and data/models/*.json of fsys, refusing unknown fields.
func Load(fsys fs.FS) (*Catalog, error) {
	var c Catalog
	if err := decode(fsys, "data/engine.json", &c.Engine); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(fsys, "data/models")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		var m Model
		if err := decode(fsys, "data/models/"+e.Name(), &m); err != nil {
			return nil, err
		}
		c.Models = append(c.Models, m)
	}
	// Standard models first, each tier in the order of the media (images, then videos) and the ID.
	rank := func(m Model) string {
		if m.Tier == TierAdvanced {
			return "1"
		}
		return "0"
	}
	slices.SortFunc(c.Models, func(a, b Model) int {
		return cmp.Compare(rank(a)+string(a.Kind)+"/"+a.ID, rank(b)+string(b.Kind)+"/"+b.ID)
	})
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
