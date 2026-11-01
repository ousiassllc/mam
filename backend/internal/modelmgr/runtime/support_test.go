package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
)

const gib = 1 << 30

func nv(name string, vram uint64, cc float64) gpu.Adapter {
	return gpu.Adapter{Name: name, VendorID: gpu.VendorNVIDIA, VRAMBytes: vram, ComputeCap: cc}
}

func report(adapters ...gpu.Adapter) gpu.Report { return gpu.Report{Adapters: adapters} }

var (
	windows   = rtm.Platform{OS: "windows", Arch: "amd64"}
	gptSoVITS = func() catalog.Runtime { r, _ := catalog.Default().Runtime("gpt-sovits"); return r }()
)

func TestEvaluateAcceptsWindowsX64WithAnNVIDIAGPUAndPicksTheBuildForItsGeneration(t *testing.T) {
	for name, tc := range map[string]struct {
		adapter gpu.Adapter
		variant catalog.Variant
	}{
		"an RTX 4070":                   {nv("NVIDIA GeForce RTX 4070", 12*gib, 8.9), catalog.VariantNVIDIA},
		"a GTX 1650 (4 GiB, the floor)": {nv("NVIDIA GeForce GTX 1650", 4*gib, 7.5), catalog.VariantNVIDIA},
		"an unknown generation":         {nv("NVIDIA Mystery", 8*gib, 0), catalog.VariantNVIDIA},
		"an RTX 5090 (Blackwell)":       {nv("NVIDIA GeForce RTX 5090", 32*gib, 12.0), catalog.VariantNVIDIA50},
	} {
		t.Run(name, func(t *testing.T) {
			s := rtm.Evaluate(gptSoVITS, windows, report(tc.adapter))
			assert.True(t, s.Supported)
			assert.Equal(t, rtm.CodeOK, s.Code)
			assert.Equal(t, tc.variant, s.Variant)
			assert.Equal(t, tc.adapter.Name, s.GPU)
			assert.Empty(t, s.Reason)
		})
	}
}

func TestEvaluateUsesTheNVIDIAAdapterWithTheMostMemory(t *testing.T) {
	s := rtm.Evaluate(gptSoVITS, windows, report(
		gpu.Adapter{Name: "AMD Radeon RX 7900", VendorID: gpu.VendorAMD, VRAMBytes: 24 * gib},
		nv("NVIDIA GeForce GTX 1060", 6*gib, 6.1),
		nv("NVIDIA GeForce RTX 5070", 12*gib, 12.0),
	))
	assert.True(t, s.Supported)
	assert.Equal(t, "NVIDIA GeForce RTX 5070", s.GPU)
	assert.Equal(t, catalog.VariantNVIDIA50, s.Variant)
}

// Every refusal says why (Japanese) and points at the cloud provider: the screen, the MCP tools and the Audio Director show the same text.
func TestEvaluateRefusesWithTheReasonAndTheCloudFallback(t *testing.T) {
	for name, tc := range map[string]struct {
		platform rtm.Platform
		rep      gpu.Report
		code     rtm.Code
		reason   string
	}{
		"macOS (Apple Silicon)":                    {rtm.Platform{OS: "darwin", Arch: "arm64"}, report(), rtm.CodeOS, "macOS では GPT-SoVITS の自動導入に対応していません"},
		"macOS with an NVIDIA name is still macOS": {rtm.Platform{OS: "darwin", Arch: "amd64"}, report(nv("NVIDIA", 16*gib, 8.9)), rtm.CodeOS, "macOS では"},
		"Linux":                 {rtm.Platform{OS: "linux", Arch: "amd64"}, report(nv("NVIDIA GeForce RTX 4090", 24*gib, 8.9)), rtm.CodeOS, "Linux では GPT-SoVITS の自動導入に対応していません"},
		"another OS":            {rtm.Platform{OS: "freebsd", Arch: "amd64"}, report(), rtm.CodeOS, "この OS では"},
		"Windows on ARM":        {rtm.Platform{OS: "windows", Arch: "arm64"}, report(nv("NVIDIA", 16*gib, 8.9)), rtm.CodeArch, "64 ビットの x86 以外"},
		"no adapter was listed": {windows, report(), rtm.CodeNoGPU, "GPU を取得できなかった"},
		"AMD only":              {windows, report(gpu.Adapter{Name: "AMD Radeon RX 7800 XT", VendorID: gpu.VendorAMD, VRAMBytes: 16 * gib}), rtm.CodeNotNVIDIA, "NVIDIA の GPU が見つからない"},
		"Intel Arc only":        {windows, report(gpu.Adapter{Name: "Intel(R) Arc(TM) A770", VendorID: gpu.VendorIntel, VRAMBytes: 16 * gib}), rtm.CodeNotNVIDIA, "AMD・Intel の GPU では使えません"},
		"a software adapter":    {windows, report(gpu.Adapter{Name: "NVIDIA Fake", VendorID: gpu.VendorNVIDIA, VRAMBytes: 16 * gib, Software: true}), rtm.CodeNotNVIDIA, "NVIDIA の GPU が見つからない"},
		"too little VRAM":       {windows, report(nv("NVIDIA GeForce GT 1030", 2*gib, 6.1)), rtm.CodeLowVRAM, "NVIDIA GeForce GT 1030 のビデオメモリは 2.0 GB で、GPT-SoVITS に必要な 4 GB に足りない"},
	} {
		t.Run(name, func(t *testing.T) {
			s := rtm.Evaluate(gptSoVITS, tc.platform, tc.rep)
			assert.False(t, s.Supported)
			assert.Equal(t, tc.code, s.Code)
			assert.Contains(t, s.Reason, tc.reason)
			assert.Contains(t, s.Fallback, "ElevenLabs")
			assert.Empty(t, s.Variant)
			assert.Contains(t, s.Message(), tc.reason)
			assert.Contains(t, s.Message(), "ElevenLabs", "the message of a refusal ends with what to use instead")
		})
	}
}

func TestChooseVariantTakesTheBaseBuildForOlderAndUnknownGPUs(t *testing.T) {
	assert.Equal(t, catalog.VariantNVIDIA, rtm.ChooseVariant(gptSoVITS, 0))
	assert.Equal(t, catalog.VariantNVIDIA, rtm.ChooseVariant(gptSoVITS, 6.1))
	assert.Equal(t, catalog.VariantNVIDIA, rtm.ChooseVariant(gptSoVITS, 9.0))
	assert.Equal(t, catalog.VariantNVIDIA50, rtm.ChooseVariant(gptSoVITS, 12.0))
	assert.Equal(t, catalog.VariantNVIDIA50, rtm.ChooseVariant(gptSoVITS, 13.0))
}
