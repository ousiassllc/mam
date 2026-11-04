// Package catalog is the list of the local models the model manager can install, and the pinned ComfyUI
// engine it downloads (spec/architecture/model-manager/model-manager.md §3.1, §4). It is data embedded in the binary
// (data/engine.json, data/models/*.json, data/runtimes/*.json, data/assets/*.json, data/overlays/*): every URL is pinned to a commit or a release tag and every file has
// its SHA-256 and size, so a download is checked against what was reviewed when the catalog was written
// (the idea of scripts/fetch-face-models.sh). A new model or a new engine version is a new mam release.
package catalog

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

//go:embed data
var dataFS embed.FS

// Variant is the build of the ComfyUI engine: the GPU it is made for.
type Variant string

// The builds of ComfyUI: the portable packages of Windows, and the build mam puts together for Apple silicon.
const (
	VariantNVIDIA      Variant = "nvidia"       // RTX 20 series and later (CUDA 13)
	VariantNVIDIACU126 Variant = "nvidia_cu126" // GTX 10 series and older (CUDA 12.6)
	VariantAMD         Variant = "amd"
	VariantIntel       Variant = "intel"
	VariantApple       Variant = "apple" // Apple silicon (M1 and later), on the GPU through Metal (MPS)
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
//
// To add a model, fill in every field (the validation and the tests of the model list fail otherwise): Commercial is the answer for the whole
// model; when the parts of the model have different licenses (a base model, a LoRA, a merge, a text encoder), list them in
// Components and give the strictest answer (modellicense.Strictest) as Commercial. URL is the license original, CheckedAt the
// date it was read.
type License struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
	// Commercial says whether commercial use is allowed (yes, conditional, no or unknown); CommercialCondition says on what condition
	// a conditional model may be used (and what limits a model that is "yes" only partly).
	Commercial          modellicense.Commercial `json:"commercial"`
	CommercialCondition string                  `json:"commercial_condition,omitempty"`
	// CheckedAt is the date (YYYY-MM-DD) the license original was read.
	CheckedAt string `json:"checked_at"`
	// Components are the licenses of the parts of the model when they differ; Commercial is the strictest of them.
	Components []LicenseComponent `json:"components,omitempty"`
	// ExcludedRegions are the regions (EU, GB, KR) where the license does not apply: the model must not be
	// used there.
	ExcludedRegions []string `json:"excluded_regions,omitempty"`
	// Notice is the sentence the consent text ends with.
	Notice string `json:"notice,omitempty"`
}

// LicenseComponent is the license of one part of a model (the base model, a LoRA, a merge, the text encoder, the VAE).
type LicenseComponent struct {
	Part       string                  `json:"part"`
	Name       string                  `json:"name"`
	URL        string                  `json:"url"`
	Commercial modellicense.Commercial `json:"commercial"`
}

// Info is the license as the screens and the MCP tools say it.
func (l License) Info() modellicense.Info {
	return modellicense.Info{
		Commercial: l.Commercial, LicenseName: l.Name, Condition: l.CommercialCondition, SourceURL: l.URL, CheckedAt: l.CheckedAt, Notice: l.Notice,
	}
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
	// Optional: the model works without the file (a LoRA of a speed mode); the install fetches it, but its absence leaves the model installed (#1025).
	Optional bool `json:"optional,omitempty"`
}

// Path is where the file lies below the base directory of the engine: models/<folder>/<name>.
func (f File) Path() string { return path.Join("models", f.Folder, f.Name) }

// VRAM is the guideline of the video memory a model needs, in GB. It is a 目安: Source says where it comes
// from, and Estimate is true when it is mam's own conservative figure with no source (not run on a GPU).
// RecommendedGB is the memory the vendor recommends when it is above MinGB (0: MinGB is also the recommendation): a GPU between the
// two runs the model with limits, and Limits says which (the quantized files, the tiled VAE, the offload, the speed).
type VRAM struct {
	MinGB         int    `json:"min_gb"`
	RecommendedGB int    `json:"recommended_gb,omitempty"`
	Limits        string `json:"limits,omitempty"`
	Source        string `json:"source"`
	Estimate      bool   `json:"estimate"`
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
	// Traits are what the model filter needs besides the license and the VRAM (#1011; package traits). What the workflows say (the inputs, the sound
	// of a video) is not in it: Derived reads that from them.
	Traits      traits.Traits `json:"traits"`
	Description string        `json:"description"`
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
	// PlatformColumns are the optional GPU generation / OS columns (Issue #979: requires, unsupported): where the model is made for and where it is
	// known not to run. They only warn (platform.Warning); nothing is refused.
	PlatformColumns
}

// PlatformColumns is platform.Columns, which the model and its view embed.
type PlatformColumns = platform.Columns

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
	// Apple is the macOS build (data/engine_apple.json; nil when the catalog has none). It is not one of Variants: the project ships no
	// portable package for macOS, so mam puts it together from pinned parts (spec/architecture/model-manager/model-manager.md §3.7).
	Apple *AppleEngine `json:"-"`
}

// Catalog is the engine, the models, the local runtimes (GPT-SoVITS; data/runtimes/*.json) and the weights and dependencies of
// the runtimes (data/assets/*.json).
type Catalog struct {
	Engine       Engine
	Models       []Model
	Runtimes     []Runtime
	ResourceSets []ResourceSet
	// Overlays are the files of data/overlays by name: the scripts mam writes into the environment of a runtime (the server of an assembled
	// runtime, which the vendor does not ship; the guarded launcher of an archive runtime). They are compiled into mam, so they need no pin.
	Overlays map[string][]byte
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

// Load reads data/engine.json, data/models/**/*.json (a sub directory keeps a group of models, such as the adult photoreal ones, out
// of the one directory that the line limit counts), data/runtimes/*.json (optional) and data/assets/*.json (optional) of fsys,
// refusing unknown fields.
func Load(fsys fs.FS) (*Catalog, error) {
	var c Catalog
	if err := decode(fsys, "data/engine.json", &c.Engine); err != nil {
		return nil, err
	}
	var err error
	if c.Engine.Apple, err = loadApple(fsys); err != nil {
		return nil, err
	}
	err = fs.WalkDir(fsys, "data/models", func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		var m Model
		if err := decode(fsys, name, &m); err != nil {
			return err
		}
		c.Models = append(c.Models, m)
		return nil
	})
	if err != nil {
		return nil, err
	}
	rts, err := fs.ReadDir(fsys, "data/runtimes")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range rts {
		var r Runtime
		if err := decode(fsys, "data/runtimes/"+e.Name(), &r); err != nil {
			return nil, err
		}
		c.Runtimes = append(c.Runtimes, r)
	}
	sets, err := fs.ReadDir(fsys, "data/assets")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range sets {
		var a ResourceSet
		if err := decode(fsys, "data/assets/"+e.Name(), &a); err != nil {
			return nil, err
		}
		c.ResourceSets = append(c.ResourceSets, a)
	}
	sortResourceSets(c.ResourceSets)
	if c.Overlays, err = loadOverlays(fsys); err != nil {
		return nil, err
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
