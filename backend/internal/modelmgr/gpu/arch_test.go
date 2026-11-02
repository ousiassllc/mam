package gpu_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// The generation of an NVIDIA GPU comes from its compute capability (the names of nvidia-smi, or the name table when nvidia-smi is missing);
// RTX 50 is Blackwell (12.0), B200 is Blackwell too (10.0), and a GPU whose capability is not known is not guessed.
func TestArchOfTheCapability(t *testing.T) {
	for cc, want := range map[float64]string{
		12.0: platform.ArchBlackwell, 10.0: platform.ArchBlackwell, 9.0: platform.ArchHopper, 8.9: platform.ArchAda, 8.6: platform.ArchAmpere, 8.0: platform.ArchAmpere,
		7.5: platform.ArchTuring, 7.0: platform.ArchVolta, 6.1: platform.ArchPascal, 5.2: platform.ArchMaxwell, 3.5: platform.ArchKepler, 0: "", 13.0: "", 11.0: "",
	} {
		assert.Equal(t, want, gpu.Arch(gpu.Adapter{VendorID: gpu.VendorNVIDIA, ComputeCap: cc}), "compute capability %.1f", cc)
	}
	assert.Equal(t, platform.ArchAMD, gpu.Arch(gpu.Adapter{VendorID: gpu.VendorAMD}))
	assert.Equal(t, platform.ArchIntel, gpu.Arch(gpu.Adapter{VendorID: gpu.VendorIntel}))
	assert.Empty(t, gpu.Arch(gpu.Adapter{VendorID: 0x1234}))
}

// The generation Detect reports is the one of the adapter with the most memory, from nvidia-smi or the name; a Mac, which has no adapter list, is
// "apple"; a software adapter and an empty list tell nothing.
func TestHostArch(t *testing.T) {
	ctx := t.Context()
	detect := func(goos string, adapters ...gpu.Adapter) string {
		return gpu.HostArch(gpu.Detect(ctx, fake{adapters: adapters}, nil, goos))
	}
	assert.Equal(t, platform.ArchBlackwell, detect("windows", nv("NVIDIA GeForce RTX 4070", 12*gib), nv("NVIDIA GeForce RTX 5090", 32*gib)), "the larger one")
	assert.Equal(t, platform.ArchAda, detect("windows", nv("NVIDIA GeForce RTX 4090", 24*gib), nv("NVIDIA GeForce RTX 5060", 8*gib)))
	assert.Equal(t, platform.ArchBlackwell, detect("windows", nv("NVIDIA RTX PRO 6000 Blackwell", 96*gib)))
	assert.Empty(t, detect("windows", nv("NVIDIA Quadro Mystery", 8*gib)), "an unknown generation is not guessed")
	assert.Equal(t, platform.ArchAMD, detect("windows", gpu.Adapter{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 * gib}))
	assert.Equal(t, platform.ArchApple, detect("darwin"))
	assert.Empty(t, detect("linux"))
	assert.Empty(t, detect("windows", gpu.Adapter{Name: "Microsoft Basic Render Driver", Software: true, VRAMBytes: 0}))
}
