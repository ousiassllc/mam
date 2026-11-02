package gpu

import "github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"

// Arch is the generation of the GPU (one of platform.Arch*; "" when it cannot be told). An NVIDIA GPU's comes from its CUDA compute
// capability (NVIDIA's table: 10.0 B200, 12.0 RTX 50 are Blackwell, 9.0 Hopper, 8.9 Ada Lovelace, 8.0 / 8.6 / 8.7 Ampere, 7.5 Turing, 7.0
// Volta); an AMD or Intel adapter has no finer generation than its vendor. An NVIDIA GPU whose compute capability is unknown is "":
// a generation guessed wrong would warn about a PC that is fine.
func Arch(a Adapter) string {
	switch a.VendorID {
	case VendorAMD:
		return platform.ArchAMD
	case VendorIntel:
		return platform.ArchIntel
	case VendorNVIDIA:
		return archOfCapability(a.ComputeCap)
	}
	return ""
}

func archOfCapability(cc float64) string {
	switch {
	case cc >= 12 && cc < 13, cc >= 10 && cc < 11:
		return platform.ArchBlackwell
	case cc >= 9 && cc < 10:
		return platform.ArchHopper
	case cc == 8.9:
		return platform.ArchAda
	case cc >= 8 && cc < 9:
		return platform.ArchAmpere
	case cc == 7.5:
		return platform.ArchTuring
	case cc == 7.0:
		return platform.ArchVolta
	case cc >= 6 && cc < 7:
		return platform.ArchPascal
	case cc >= 5 && cc < 6:
		return platform.ArchMaxwell
	case cc >= 3 && cc < 4:
		return platform.ArchKepler
	}
	return ""
}

// HostArch is the generation of the GPU of the report that a model is judged against: the adapter with the most video memory (the one
// hfplan.VRAMOf and the recommendation choose by), and on macOS, where the detector lists no adapter, the Mac's own GPU.
func HostArch(r Report) string {
	var best *Adapter
	for i := range r.Adapters {
		if a := &r.Adapters[i]; !a.Software && (best == nil || a.VRAMBytes > best.VRAMBytes) {
			best = a
		}
	}
	switch {
	case best != nil:
		return Arch(*best)
	case r.OS == platform.OSDarwin:
		return platform.ArchApple
	}
	return ""
}
