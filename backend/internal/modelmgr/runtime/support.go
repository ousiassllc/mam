// Package runtime installs and describes a local AI runtime (GPT-SoVITS; MuseTalk later) that mam starts and stops by itself, the
// same way as the bundled ComfyUI engine (spec/architecture/model-manager/local-runtime.md): Evaluate says whether this machine can
// run it, Installer downloads the pinned package of the vendor from its official host, checks its SHA-256, extracts it next to
// mam.exe and builds the launch of its server. mam ships none of the runtime's bytes. The kind of the runtime is catalog data
// (catalog.Runtime), so the code here knows nothing of GPT-SoVITS.
package runtime

import (
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// Code is the machine-readable answer of Evaluate: the screen, the MCP tools and the Audio Director branch on it and show Reason.
type Code string

// The codes of Support.
const (
	CodeOK        Code = "ok"
	CodeOS        Code = "os"         // the OS is not the one mam installs the runtime on (macOS, Linux)
	CodeArch      Code = "arch"       // not x64
	CodeNoGPU     Code = "no_gpu"     // no display adapter could be listed
	CodeNotNVIDIA Code = "not_nvidia" // there is no NVIDIA adapter (AMD, Intel)
	CodeLowVRAM   Code = "low_vram"   // the NVIDIA adapter has less video memory than the runtime needs
)

// Platform is the OS and the CPU architecture mam runs on (runtime.GOOS and GOARCH; a test passes others).
type Platform struct{ OS, Arch string }

// Support is whether this machine can run the runtime, and why not. A refusal carries the sentence for the user and what to use instead
// (the cloud provider), so that every screen says the same.
type Support struct {
	Supported bool `json:"supported"`
	Code      Code `json:"code"`
	// Reason says (Japanese) why the runtime cannot be installed here ("" when it can). Fallback says what to use instead.
	Reason   string `json:"reason,omitempty"`
	Fallback string `json:"fallback,omitempty"`
	// Variant is the build to install, and GPU the adapter it is for ("" unless Supported).
	Variant catalog.Variant `json:"variant,omitempty"`
	GPU     string          `json:"gpu,omitempty"`
}

// Message is Reason followed by Fallback: the text of a refusal.
func (s Support) Message() string { return strings.TrimSpace(s.Reason + " " + s.Fallback) }

// Evaluate decides whether rt can be installed and started on a machine with platform p and the adapters of rep. The decision is a
// pure function of those facts (a test passes any platform and any adapters). Supported is the OS and architecture of rt.Support and an
// NVIDIA adapter with at least rt.VRAM.MinGB of dedicated memory; everything else is refused with its reason and the cloud fallback.
func Evaluate(rt catalog.Runtime, p Platform, rep gpu.Report) Support {
	s := rt.Support
	refuse := func(code Code, reason string) Support {
		return Support{Code: code, Reason: reason, Fallback: s.Fallback}
	}
	if p.OS != s.OS {
		reason, ok := s.OSReasons[p.OS]
		if !ok {
			reason = s.OSReasons["default"]
		}
		return refuse(CodeOS, reason)
	}
	if p.Arch != s.Arch {
		return refuse(CodeArch, s.ArchReason)
	}
	if len(rep.Adapters) == 0 {
		return refuse(CodeNoGPU, s.NoGPUReason)
	}
	best := bestNVIDIA(rep.Adapters)
	if best == nil {
		return refuse(CodeNotNVIDIA, s.NotNVIDIAReason)
	}
	if best.VRAMBytes < uint64(rt.VRAM.MinGB)<<30 { //nolint:gosec // a positive, validated size
		return refuse(CodeLowVRAM, strings.NewReplacer(
			"{gpu}", best.Name, "{vram_gb}", fmt.Sprintf("%.1f", float64(best.VRAMBytes)/(1<<30)), "{min_vram_gb}", fmt.Sprint(rt.VRAM.MinGB),
		).Replace(s.LowVRAMReason))
	}
	return Support{Supported: true, Code: CodeOK, Variant: ChooseVariant(rt, best.ComputeCap), GPU: best.Name}
}

// bestNVIDIA is the NVIDIA adapter with the most dedicated memory (nil for none; a software adapter never counts).
func bestNVIDIA(adapters []gpu.Adapter) *gpu.Adapter {
	var best *gpu.Adapter
	for i := range adapters {
		a := &adapters[i]
		if a.VendorID == gpu.VendorNVIDIA && !a.Software && (best == nil || a.VRAMBytes > best.VRAMBytes) {
			best = a
		}
	}
	return best
}

// ChooseVariant is the build of rt for an NVIDIA GPU of compute capability cc (0: unknown): the one with the highest MinComputeCap
// that cc reaches, which is the base build (MinComputeCap 0) for an older or unknown GPU.
func ChooseVariant(rt catalog.Runtime, cc float64) catalog.Variant {
	chosen := catalog.Variant("")
	for _, v := range rt.VariantList() { // the lowest MinComputeCap first
		if rt.Variants[v].MinComputeCap <= cc {
			chosen = v
		}
	}
	return chosen
}
