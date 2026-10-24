// Package gpu finds the GPU of the machine and chooses the ComfyUI portable build for it
// (spec/architecture/model-manager/model-manager.md §3.2). The adapter list comes from a Detector (DXGI on Windows; nothing elsewhere), the
// architecture of an NVIDIA GPU from nvidia-smi or, failing that, from the adapter name. The choice is a pure
// function of those facts (Recommend), so it is tested with fakes; the real detection only runs on Windows.
package gpu

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

// PCI vendor IDs.
const (
	VendorNVIDIA = 0x10DE
	VendorAMD    = 0x1002
	VendorIntel  = 0x8086
)

// minVRAM is the dedicated video memory below which an adapter is treated as an integrated GPU: the
// portable builds are for discrete GPUs (an Intel UHD / an AMD APU shares system memory).
const minVRAM = 4 << 30

// Adapter is a display adapter as DXGI lists it.
type Adapter struct {
	Name     string `json:"name"`
	VendorID uint32 `json:"vendor_id"`
	// VRAMBytes is the dedicated video memory (DedicatedVideoMemory, 64 bit).
	VRAMBytes uint64 `json:"vram_bytes"`
	// Software marks a software adapter (the Microsoft Basic Render Driver): never a candidate.
	Software bool `json:"software,omitempty"`
}

// Detector lists the display adapters of the machine.
type Detector interface {
	Adapters(ctx context.Context) ([]Adapter, error)
}

// Recommendation is the build to install, or the reason there is none.
type Recommendation struct {
	// Variant is the portable build ("" when no supported GPU was found).
	Variant catalog.Variant `json:"variant"`
	// Adapter is the GPU the choice is for.
	Adapter *Adapter `json:"adapter,omitempty"`
	// Confidence is "high" (the architecture is known) or "low" (guessed; the user should confirm).
	Confidence string `json:"confidence,omitempty"`
	// Reason says why, for the screen.
	Reason string `json:"reason"`
	// CPUAvailable: without a supported GPU the engine can still run on the CPU, but only when the user asks
	// for it explicitly (it is very slow, unusable for video). It is never chosen automatically.
	CPUAvailable bool `json:"cpu_available"`
	// Guidance is what to do when Variant is empty.
	Guidance string `json:"guidance,omitempty"`
}

// Report is what --detect-gpu prints.
type Report struct {
	OS             string         `json:"os"`
	Adapters       []Adapter      `json:"adapters"`
	Recommendation Recommendation `json:"recommendation"`
	// Error is why the adapters could not be listed ("" when they could).
	Error string `json:"error,omitempty"`
}

const noGPUGuidance = "対応する GPU が見つかりません。ネットワーク上の（または別のアプリで動かしている）ComfyUI の URL を設定して使うか、遅いことを承知のうえで CPU 実行を明示的に選んでください。"

// Detect lists the adapters with d and recommends a build. caps are the NVIDIA compute capabilities by
// adapter name from nvidia-smi (may be nil).
func Detect(ctx context.Context, d Detector, caps map[string]float64, goos string) Report {
	r := Report{OS: goos, Adapters: []Adapter{}}
	adapters, err := d.Adapters(ctx)
	if err != nil {
		r.Error = err.Error()
	}
	r.Adapters = append(r.Adapters, adapters...)
	r.Recommendation = Recommend(adapters, caps)
	return r
}

// Recommend chooses the build for the best supported adapter: NVIDIA (the CUDA 13 build for Turing, RTX 20
// series, and later, CUDA 12.6 for older), AMD (Radeon) and Intel (Arc). An adapter with less than 4 GiB of
// dedicated memory (an integrated GPU) and a software adapter are not supported. The adapter with the most
// video memory wins.
func Recommend(adapters []Adapter, caps map[string]float64) Recommendation {
	var best *Adapter
	for i := range adapters {
		a := &adapters[i]
		if supported(*a) && (best == nil || a.VRAMBytes > best.VRAMBytes) {
			best = a
		}
	}
	if best == nil {
		return Recommendation{Reason: noSupportedReason(adapters), CPUAvailable: true, Guidance: noGPUGuidance}
	}
	rec := Recommendation{Adapter: best, Confidence: "high"}
	switch best.VendorID {
	case VendorNVIDIA:
		cc, known := capability(*best, caps)
		rec.Variant = catalog.VariantNVIDIA
		switch {
		case !known:
			rec.Confidence = "low"
			rec.Reason = fmt.Sprintf("NVIDIA %s: 世代を判別できないため、新しい世代向け（CUDA 13）のビルドを選びました。起動しない場合は旧世代向けを選んでください。", best.Name)
		case cc < 7.5:
			rec.Variant = catalog.VariantNVIDIACU126
			rec.Reason = fmt.Sprintf("NVIDIA %s（Compute Capability %.1f、Turing より前の世代）: CUDA 12.6 のビルド。", best.Name, cc)
		default:
			rec.Reason = fmt.Sprintf("NVIDIA %s（Compute Capability %.1f）: CUDA 13 のビルド。", best.Name, cc)
		}
	case VendorAMD:
		rec.Variant, rec.Reason = catalog.VariantAMD, fmt.Sprintf("AMD %s: ROCm のビルド。", best.Name)
	default:
		rec.Variant, rec.Reason = catalog.VariantIntel, fmt.Sprintf("Intel %s: XPU のビルド。", best.Name)
	}
	return rec
}

func supported(a Adapter) bool {
	if a.Software || a.VRAMBytes < minVRAM {
		return false
	}
	switch a.VendorID {
	case VendorNVIDIA, VendorAMD:
		return true
	case VendorIntel:
		return strings.Contains(strings.ToLower(a.Name), "arc") // an Arc card; an Intel UHD / Iris iGPU is not
	}
	return false
}

func noSupportedReason(adapters []Adapter) string {
	if len(adapters) == 0 {
		return "GPU を取得できませんでした。"
	}
	return "対応する GPU（VRAM 4 GB 以上の NVIDIA・AMD Radeon・Intel Arc）がありません。"
}
