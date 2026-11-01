package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
)

var museTalk = func() catalog.Runtime { r, _ := catalog.Default().Runtime("musetalk"); return r }()

func TestEvaluateAcceptsMuseTalkOnWindowsWithAnNVIDIAGPUOfTheGenerationsPyTorch2Knows(t *testing.T) {
	for name, adapter := range map[string]gpu.Adapter{
		"a GTX 1650 (4 GiB, the machine the README tested)": nv("NVIDIA GeForce GTX 1650", 4*gib, 7.5),
		"an RTX 4090":           nv("NVIDIA GeForce RTX 4090", 24*gib, 8.9),
		"an unknown generation": nv("NVIDIA Mystery", 8*gib, 0),
	} {
		t.Run(name, func(t *testing.T) {
			s := rtm.Evaluate(museTalk, windows, report(adapter))
			assert.True(t, s.Supported)
			assert.Equal(t, catalog.VariantNVIDIA, s.Variant)
		})
	}
}

// The environment of the official recipe is PyTorch 2.0.1 with CUDA 11.8: it has no kernels for the RTX 50 series, so mam refuses it
// rather than install an environment that fails when the model is moved to the GPU.
func TestEvaluateRefusesMuseTalkOnTheRTX50SeriesAndSaysWhy(t *testing.T) {
	s := rtm.Evaluate(museTalk, windows, report(nv("NVIDIA GeForce RTX 5090", 32*gib, 12.0)))
	assert.False(t, s.Supported)
	assert.Equal(t, rtm.CodeComputeCap, s.Code)
	assert.Contains(t, s.Reason, "NVIDIA GeForce RTX 5090 は NVIDIA の RTX 50 シリーズ")
	assert.Contains(t, s.Reason, "PyTorch 2.0.1")
	assert.Contains(t, s.Message(), "Sync Labs")
	assert.Empty(t, s.Variant)
}

func TestEvaluateRefusesMuseTalkWhereItCannotRunAndPointsAtSyncLabs(t *testing.T) {
	for name, tc := range map[string]struct {
		platform rtm.Platform
		rep      gpu.Report
		code     rtm.Code
		reason   string
	}{
		"macOS": {rtm.Platform{OS: "darwin", Arch: "arm64"}, report(), rtm.CodeOS, "macOS では MuseTalk 1.5 を自動導入できません。公式の PyTorch 版は macOS（Apple Silicon）の GPU に対応しておらず"},
		"macOS names the MLX build it does not install": {rtm.Platform{OS: "darwin", Arch: "arm64"}, report(), rtm.CodeOS, "mlx-community/MuseTalk-1.5-q4"},
		"Linux":     {rtm.Platform{OS: "linux", Arch: "amd64"}, report(nv("NVIDIA GeForce RTX 4090", 24*gib, 8.9)), rtm.CodeOS, "Linux では MuseTalk 1.5"},
		"AMD":       {windows, report(gpu.Adapter{Name: "AMD Radeon RX 7800 XT", VendorID: gpu.VendorAMD, VRAMBytes: 16 * gib}), rtm.CodeNotNVIDIA, "AMD・Intel の GPU では使えません"},
		"no GPU":    {windows, report(), rtm.CodeNoGPU, "GPU を取得できなかった"},
		"2 GiB GPU": {windows, report(nv("NVIDIA GeForce GT 1030", 2*gib, 6.1)), rtm.CodeLowVRAM, "MuseTalk 1.5 に必要な 4 GB に足りない"},
	} {
		t.Run(name, func(t *testing.T) {
			s := rtm.Evaluate(museTalk, tc.platform, tc.rep)
			assert.False(t, s.Supported)
			assert.Equal(t, tc.code, s.Code)
			assert.Contains(t, s.Reason, tc.reason)
			assert.Contains(t, s.Message(), "Sync Labs", "what to use instead is the cloud lip-sync")
			assert.NotContains(t, s.Message(), "ElevenLabs")
		})
	}
}
