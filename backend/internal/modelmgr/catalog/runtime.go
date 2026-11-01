package catalog

import (
	"cmp"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/parts"
)

// Purpose says what a local runtime makes: speech (TTS, voice cloning) or a lip-synced video. The runtime kind is data (data/runtimes/*.json),
// so a second runtime (MuseTalk) is a new file of the same shape, not new code (spec/architecture/model-manager/local-runtime.md §3).
type Purpose string

// The purposes of a runtime.
const (
	PurposeTTS     Purpose = "tts"
	PurposeLipSync Purpose = "lip_sync"
)

// VariantNVIDIA50 is the extra build of a runtime for the newest NVIDIA generation (RTX 50 series, compute capability 12.0 and up).
const VariantNVIDIA50 Variant = "nvidia50"

// RuntimeVariant is a pinned build of a runtime. MinComputeCap is the NVIDIA compute capability the build is for: the build with
// the highest MinComputeCap that the GPU reaches is installed (0: the build for every other supported GPU). A build is either one
// archive (Asset: the vendor's package, a 7z) or Parts that mam puts together (Asset then holds the label and the sums of the parts).
type RuntimeVariant struct {
	Asset
	MinComputeCap float64      `json:"min_compute_cap,omitempty"`
	Parts         []parts.Part `json:"parts,omitempty"`
}

// Assembled reports whether the build is put together from parts (not one archive).
func (v RuntimeVariant) Assembled() bool { return len(v.Parts) > 0 }

// RuntimeLaunch is how the installed runtime is started: the interpreter and the arguments (relative to the extracted directory), the
// path that answers once the server is up, and the environment that keeps it from fetching anything by itself.
type RuntimeLaunch struct {
	// Python is the interpreter, relative to the runtime's environment directory. PathDirs are the directories (relative, slash
	// separated) that are put in front of PATH, as the vendor's launcher does.
	Python   string   `json:"python"`
	PathDirs []string `json:"path_dirs,omitempty"`
	// Entry is the server script (relative): the vendor's, or mam's own launcher that the install writes (Runtime.ExtraFiles); the
	// install checks it is in the environment, and Args must run it.
	Entry string `json:"entry"`
	// Args are the arguments of Python; "{port}" is replaced by the port. The server must bind 127.0.0.1 only (validated).
	Args []string `json:"args"`
	// ReadyPath is the path that answers 200 once the server is ready (the models are loaded before the port opens).
	ReadyPath string `json:"ready_path"`
	// Env is added to the environment ("NAME=value"); it turns the automatic downloads of the libraries off (offline mode).
	Env []string `json:"env"`
	// StartTimeoutSec bounds the wait for the server to answer (the first start loads the models).
	StartTimeoutSec int `json:"start_timeout_sec"`
	// TokenEnv names the environment variable that carries the random token of this start ("" for none): a server that writes files
	// on request answers only the caller that has it (a web page cannot send it).
	TokenEnv string `json:"token_env,omitempty"`
}

// RuntimeSupport says where a runtime can be installed and, when it cannot, why (Japanese, shown by the screen, the MCP tools and the
// Audio Director alike) and what to use instead. A reason with "{gpu}", "{vram_gb}" or "{min_vram_gb}" has them filled in.
type RuntimeSupport struct {
	// OS and Arch are the only platform mam installs the runtime on (GOOS and GOARCH values).
	OS   string `json:"os"`
	Arch string `json:"arch"`
	// OSReasons are the reasons by GOOS; "default" is the reason of any other OS.
	OSReasons  map[string]string `json:"os_reasons"`
	ArchReason string            `json:"arch_reason"`
	// NoGPUReason: no adapter could be listed. NotNVIDIAReason: there is no NVIDIA adapter. LowVRAMReason: the NVIDIA adapter with the
	// most memory has less than the runtime's VRAM guideline.
	NoGPUReason     string `json:"no_gpu_reason"`
	NotNVIDIAReason string `json:"not_nvidia_reason"`
	LowVRAMReason   string `json:"low_vram_reason"`
	// MaxComputeCap, when set, is the first NVIDIA compute capability the runtime does not support (its libraries are built for the
	// older GPUs); ComputeCapReason says why (it names {gpu}). A GPU whose capability is unknown is not refused.
	MaxComputeCap    float64 `json:"max_compute_cap,omitempty"`
	ComputeCapReason string  `json:"compute_cap_reason,omitempty"`
	// Fallback says what to do instead: the cloud provider that makes the same thing.
	Fallback string `json:"fallback"`
}

// Runtime is a local AI runtime that mam installs, starts and stops by itself (the same way as the bundled ComfyUI engine): a pinned
// archive of the vendor's own program, its launch and where it is supported. mam ships none of its bytes.
type Runtime struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Purpose Purpose `json:"purpose"`
	// Version is the pinned version of the package; installing again after a newer release pins another version is the update.
	Version   string `json:"version"`
	SourceURL string `json:"source_url"`
	// Description says what the package holds (shown before the install).
	Description string `json:"description"`
	// ArchiveRoot is the single directory inside the archives; its content is the runtime.
	ArchiveRoot string        `json:"archive_root"`
	Launch      RuntimeLaunch `json:"launch"`
	// VRAM is the video memory the runtime needs; a GPU with less is refused.
	VRAM    VRAM           `json:"vram"`
	Support RuntimeSupport `json:"support"`
	License License        `json:"license"`
	// Build says how the parts of an assembled variant become the environment (nil when every variant is one archive).
	Build *parts.Build `json:"build,omitempty"`
	// ExtraFiles are the files mam writes into the extracted environment of an archive runtime (a runtime without Build): its own
	// launcher script next to the vendor's server, which the install writes after every extraction and Installer.Command writes again before every start
	// (the current one of this mam, without a new download of the package).
	ExtraFiles []parts.File `json:"extra_files,omitempty"`
	// Variants are the pinned builds by the GPU generation they are for.
	Variants map[Variant]RuntimeVariant `json:"variants"`
}

// VariantList lists the builds of the runtime: the one with the lowest MinComputeCap first, then by name.
func (r Runtime) VariantList() []Variant {
	names := make([]Variant, 0, len(r.Variants))
	for v := range r.Variants {
		names = append(names, v)
	}
	slices.SortFunc(names, func(a, b Variant) int {
		return cmp.Or(cmp.Compare(r.Variants[a].MinComputeCap, r.Variants[b].MinComputeCap), cmp.Compare(a, b))
	})
	return names
}

// Runtime returns the runtime with the ID.
func (c *Catalog) Runtime(id string) (Runtime, bool) {
	i := slices.IndexFunc(c.Runtimes, func(r Runtime) bool { return r.ID == id })
	if i < 0 {
		return Runtime{}, false
	}
	return c.Runtimes[i], true
}
