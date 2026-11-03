package gpu_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

func appleRunner(brand, memsize string) *fakeRunner {
	return &fakeRunner{out: map[string]string{
		"sysctl -n machdep.cpu.brand_string": brand + "\n",
		"sysctl -n hw.memsize":               memsize + "\n",
	}}
}

func TestAppleDetectorListsTheChipWithTheUnifiedMemory(t *testing.T) {
	adapters, err := gpu.Apple{Runner: appleRunner("Apple M2 Pro", "17179869184")}.Adapters(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []gpu.Adapter{{Name: "Apple M2 Pro", VendorID: gpu.VendorApple, VRAMBytes: 16 * gib}}, adapters)
}

func TestAnIntelMacHasNoAppleGPUToUse(t *testing.T) {
	r := appleRunner("Intel(R) Core(TM) i9-9980HK CPU @ 2.40GHz", "34359738368")
	adapters, err := gpu.Apple{Runner: r}.Adapters(context.Background())
	require.NoError(t, err)
	assert.Empty(t, adapters)
	assert.Equal(t, []string{"sysctl -n machdep.cpu.brand_string"}, r.calls, "the memory of an Intel Mac is not read")
}

func TestAppleDetectorReportsWhatItCouldNotRead(t *testing.T) {
	for name, tc := range map[string]struct {
		runner *fakeRunner
		want   string
	}{
		"no sysctl":      {&fakeRunner{}, "machdep.cpu.brand_string"},
		"no memory size": {&fakeRunner{out: map[string]string{"sysctl -n machdep.cpu.brand_string": "Apple M1\n"}}, "hw.memsize"},
		"junk memory":    {appleRunner("Apple M1", "lots"), "unreadable output"},
		"zero memory":    {appleRunner("Apple M1", "0"), "unreadable output"},
	} {
		t.Run(name, func(t *testing.T) {
			adapters, err := gpu.Apple{Runner: tc.runner}.Adapters(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, adapters)
		})
	}
}

// An Apple silicon Mac is recommended the macOS build; an 8 GB MacBook Air counts (it is above the 4 GiB of an integrated GPU).
func TestAnAppleSiliconMacGetsTheMacOSBuild(t *testing.T) {
	for _, mem := range []uint64{8 * gib, 16 * gib, 128 * gib} {
		rep := gpu.Detect(context.Background(), fakeDetector{adapters: []gpu.Adapter{{Name: "Apple M3", VendorID: gpu.VendorApple, VRAMBytes: mem}}}, nil, "darwin")
		assert.Equal(t, catalog.VariantApple, rep.Recommendation.Variant, mem)
		assert.Equal(t, "high", rep.Recommendation.Confidence)
		assert.Contains(t, rep.Recommendation.Reason, "Apple M3")
		assert.Contains(t, rep.Recommendation.Reason, "MPS")
		assert.Zero(t, rep.Adapters[0].ComputeCap, "compute capability is NVIDIA's")
		assert.Equal(t, "darwin", rep.OS)
	}
}

func TestAMacWithoutAGPUListedHasNoBuildToInstall(t *testing.T) {
	rep := gpu.Detect(context.Background(), fakeDetector{}, nil, "darwin")
	assert.Empty(t, rep.Recommendation.Variant)
	assert.True(t, rep.Recommendation.CPUAvailable)
	assert.Equal(t, platform.ArchApple, gpu.HostArch(rep), "a Mac is judged as Apple silicon even when sysctl could not be read")
}

func TestTheGenerationOfAnAppleAdapterIsApple(t *testing.T) {
	a := gpu.Adapter{Name: "Apple M4", VendorID: gpu.VendorApple, VRAMBytes: 16 * gib}
	assert.Equal(t, platform.ArchApple, gpu.Arch(a))
	assert.Equal(t, platform.ArchApple, gpu.HostArch(gpu.Report{OS: "darwin", Adapters: []gpu.Adapter{a}}))
}

func TestMacOSMajorReadsTheProductVersion(t *testing.T) {
	for in, want := range map[string]int{"14.6.1\n": 14, "15.0\n": 15, "26.1": 26, "13.7.8": 13} {
		n, err := gpu.MacOSMajor(context.Background(), &fakeRunner{out: map[string]string{"sw_vers -productVersion": in}})
		require.NoError(t, err, in)
		assert.Equal(t, want, n, in)
	}
	for name, r := range map[string]*fakeRunner{
		"no sw_vers": {},
		"junk":       {out: map[string]string{"sw_vers -productVersion": "Sonoma"}},
		"empty":      {out: map[string]string{"sw_vers -productVersion": "\n"}},
		"failing":    {err: map[string]error{"sw_vers": errors.New("boom")}},
	} {
		_, err := gpu.MacOSMajor(context.Background(), r)
		assert.Error(t, err, name)
	}
}
