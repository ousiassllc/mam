package gpu_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

const gib = 1 << 30

func nv(name string, vram uint64) gpu.Adapter {
	return gpu.Adapter{Name: name, VendorID: gpu.VendorNVIDIA, VRAMBytes: vram}
}

func TestRecommendByVendorAndGeneration(t *testing.T) {
	for name, tc := range map[string]struct {
		adapters []gpu.Adapter
		caps     map[string]float64
		variant  catalog.Variant
		conf     string
	}{
		"rtx 4090 by name":       {[]gpu.Adapter{nv("NVIDIA GeForce RTX 4090", 24*gib)}, nil, catalog.VariantNVIDIA, "high"},
		"rtx 2060 by name":       {[]gpu.Adapter{nv("NVIDIA GeForce RTX 2060", 6*gib)}, nil, catalog.VariantNVIDIA, "high"},
		"gtx 1660 is turing":     {[]gpu.Adapter{nv("NVIDIA GeForce GTX 1660 SUPER", 6*gib)}, nil, catalog.VariantNVIDIA, "high"},
		"gtx 1080 is pascal":     {[]gpu.Adapter{nv("NVIDIA GeForce GTX 1080", 8*gib)}, nil, catalog.VariantNVIDIACU126, "high"},
		"titan v is volta":       {[]gpu.Adapter{nv("NVIDIA TITAN V", 12*gib)}, nil, catalog.VariantNVIDIACU126, "high"},
		"rtx 5090":               {[]gpu.Adapter{nv("NVIDIA GeForce RTX 5090", 32*gib)}, nil, catalog.VariantNVIDIA, "high"},
		"rtx a4000":              {[]gpu.Adapter{nv("NVIDIA RTX A4000", 16*gib)}, nil, catalog.VariantNVIDIA, "high"},
		"quadro p4000":           {[]gpu.Adapter{nv("Quadro P4000", 8*gib)}, nil, catalog.VariantNVIDIACU126, "high"},
		"smi overrides the name": {[]gpu.Adapter{nv("NVIDIA Mystery 9000", 8*gib)}, map[string]float64{"NVIDIA Mystery 9000": 6.1}, catalog.VariantNVIDIACU126, "high"},
		"unknown name":           {[]gpu.Adapter{nv("NVIDIA Mystery 9000", 8*gib)}, nil, catalog.VariantNVIDIA, "low"},
		"amd radeon":             {[]gpu.Adapter{{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 * gib}}, nil, catalog.VariantAMD, "high"},
		"intel arc":              {[]gpu.Adapter{{Name: "Intel(R) Arc(TM) A770 Graphics", VendorID: gpu.VendorIntel, VRAMBytes: 16 * gib}}, nil, catalog.VariantIntel, "high"},
		"the biggest wins": {[]gpu.Adapter{
			{Name: "Intel(R) UHD Graphics 770", VendorID: gpu.VendorIntel, VRAMBytes: 128 << 20},
			nv("NVIDIA GeForce RTX 3060", 12*gib), nv("NVIDIA GeForce RTX 3090", 24*gib),
		}, nil, catalog.VariantNVIDIA, "high"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := gpu.Recommend(tc.adapters, tc.caps)
			assert.Equal(t, tc.variant, rec.Variant, rec.Reason)
			assert.Equal(t, tc.conf, rec.Confidence)
			assert.NotEmpty(t, rec.Reason)
			require.NotNil(t, rec.Adapter)
		})
	}
}

// No supported GPU: nothing is chosen automatically (the CPU is offered, never picked), with guidance.
func TestRecommendChoosesNothingWithoutASupportedGPU(t *testing.T) {
	for name, adapters := range map[string][]gpu.Adapter{
		"none":              nil,
		"intel igpu":        {{Name: "Intel(R) UHD Graphics", VendorID: gpu.VendorIntel, VRAMBytes: 128 << 20}},
		"intel core ultra":  {{Name: "Intel(R) Arc(TM) Graphics", VendorID: gpu.VendorIntel, VRAMBytes: 256 << 20}},
		"amd apu":           {{Name: "AMD Radeon(TM) Graphics", VendorID: gpu.VendorAMD, VRAMBytes: 512 << 20}},
		"software adapter":  {{Name: "Microsoft Basic Render Driver", VendorID: 0x1414, Software: true}},
		"a tiny nvidia":     {nv("NVIDIA GeForce GT 710", 1*gib)},
		"an unknown vendor": {{Name: "Mystery", VendorID: 0x1234, VRAMBytes: 16 * gib}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := gpu.Recommend(adapters, nil)
			assert.Empty(t, rec.Variant)
			assert.Nil(t, rec.Adapter)
			assert.True(t, rec.CPUAvailable)
			assert.Contains(t, rec.Guidance, "ComfyUI の URL")
		})
	}
}

func TestParseSmi(t *testing.T) {
	caps := gpu.ParseSmi("NVIDIA GeForce RTX 4090, 8.9\r\nNVIDIA GeForce GTX 1080, 6.1\nbroken line\nName, [N/A]\n\n")
	assert.Equal(t, map[string]float64{"NVIDIA GeForce RTX 4090": 8.9, "NVIDIA GeForce GTX 1080": 6.1}, caps)
	assert.Empty(t, gpu.ParseSmi(""))
}

type fake struct {
	adapters []gpu.Adapter
	err      error
}

func (f fake) Adapters(context.Context) ([]gpu.Adapter, error) { return f.adapters, f.err }

func TestDetectReportsAdaptersAndTheFailureToList(t *testing.T) {
	r := gpu.Detect(context.Background(), fake{adapters: []gpu.Adapter{nv("NVIDIA GeForce RTX 4070", 12*gib)}}, nil, "windows")
	assert.Equal(t, "windows", r.OS)
	assert.Len(t, r.Adapters, 1)
	assert.Equal(t, catalog.VariantNVIDIA, r.Recommendation.Variant)
	assert.Empty(t, r.Error)

	r = gpu.Detect(context.Background(), fake{err: errors.New("dxgi.dll: not found")}, nil, "windows")
	assert.Equal(t, "dxgi.dll: not found", r.Error)
	assert.NotNil(t, r.Adapters, "an empty list, not null, in the JSON")
	assert.Empty(t, r.Recommendation.Variant)
}
