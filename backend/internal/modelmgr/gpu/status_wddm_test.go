package gpu_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// The Windows performance counters (PowerShell + Get-CimInstance, CSV). The fixtures follow the format of the WMI classes
// Win32_PerfFormattedData_GPUPerformanceCounters_GPUAdapterMemory / _GPUEngine as ConvertTo-Csv prints them; they are written
// by hand from that format, not captured on a Windows machine.
const (
	psMemory = "powershell.exe -NoProfile -NonInteractive -Command $ErrorActionPreference = 'Stop'; Get-CimInstance -ClassName Win32_PerfFormattedData_GPUPerformanceCounters_GPUAdapterMemory"
	psEngine = "powershell.exe -NoProfile -NonInteractive -Command $ErrorActionPreference = 'Stop'; Get-CimInstance -ClassName Win32_PerfFormattedData_GPUPerformanceCounters_GPUEngine"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}

func windowsAdapters() fakeDetector {
	return fakeDetector{adapters: []gpu.Adapter{
		{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 << 30, LUID: 0xe8f1},
		{Name: "AMD Radeon(TM) Graphics", VendorID: gpu.VendorAMD, VRAMBytes: 512 << 20, LUID: 0xe9a2},
		{Name: "Intel(R) Arc(TM) A770", VendorID: gpu.VendorIntel, VRAMBytes: 16 << 30, LUID: 0x10a5c},
		{Name: "Microsoft Basic Render Driver", VendorID: 0x1414, Software: true, LUID: 0x1},
	}}
}

func TestStatusWindowsCountersGiveAmdAndIntelTheirUsage(t *testing.T) {
	r := &fakeRunner{out: map[string]string{psMemory: fixture(t, "wddm_adapter_memory.csv"), psEngine: fixture(t, "wddm_engine.csv")}}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "windows", GOARCH: "amd64", Runner: r, Detector: windowsAdapters()})

	assert.Equal(t, "windows-counters", st.Source)
	require.Len(t, st.GPUs, 3, "a software adapter is not a GPU")

	xtx := st.GPUs[0]
	assert.EqualValues(t, int64(11811160064), *xtx.VRAMUsedBytes)
	assert.EqualValues(t, int64(24)<<30-11811160064, *xtx.VRAMFreeBytes)
	assert.Equal(t, 71, *xtx.UtilizationPercent, "the 3D engine of two processes (41 + 30) is the busiest engine")
	assert.Nil(t, xtx.TemperatureCelsius)
	assert.Contains(t, xtx.Unavailable["temperature_celsius"], "温度")
	assert.NotContains(t, xtx.Unavailable, "vram_used_bytes")
	assert.NotContains(t, xtx.Unavailable, "utilization_percent")
	assert.Contains(t, xtx.Note, "パフォーマンスカウンター")

	apu := st.GPUs[1]
	assert.EqualValues(t, int64(256)<<20, *apu.VRAMUsedBytes)
	assert.EqualValues(t, int64(256)<<20, *apu.VRAMFreeBytes)
	assert.Equal(t, 9, *apu.UtilizationPercent, "an idle 3D engine does not hide the video decoder")

	arc := st.GPUs[2]
	assert.EqualValues(t, int64(500)<<20, *arc.VRAMUsedBytes)
	assert.Nil(t, arc.UtilizationPercent, "no GPU Engine instance for this adapter: no number, a reason")
	assert.Contains(t, arc.Unavailable["utilization_percent"], "GPU Engine")
}

func TestStatusWindowsCountersFailOnTheirOwn(t *testing.T) {
	// The memory set cannot be read (Windows without the counters); the engine set can.
	r := &fakeRunner{
		out: map[string]string{psEngine: fixture(t, "wddm_engine.csv")},
		err: map[string]error{psMemory: errors.New("exit status 1")},
	}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "windows", GOARCH: "amd64", Runner: r, Detector: windowsAdapters()})
	g := st.GPUs[0]
	assert.Nil(t, g.VRAMUsedBytes)
	assert.Nil(t, g.VRAMFreeBytes)
	assert.Contains(t, g.Unavailable["vram_used_bytes"], "exit status 1")
	assert.Contains(t, g.Unavailable["vram_free_bytes"], "GPU Adapter Memory")
	assert.Equal(t, 71, *g.UtilizationPercent)
	assert.Equal(t, "windows-counters", st.Source)
}

func TestStatusWindowsCountersUnreadableLeavesTheReasons(t *testing.T) {
	r := &fakeRunner{out: map[string]string{psMemory: "\"Name\",\"DedicatedUsage\"\r\n", psEngine: "not csv at all\r\n"}}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "windows", GOARCH: "amd64", Runner: r, Detector: windowsAdapters()})
	assert.Equal(t, "adapters", st.Source)
	g := st.GPUs[0]
	assert.EqualValues(t, int64(24)<<30, *g.VRAMTotalBytes)
	assert.Nil(t, g.VRAMUsedBytes)
	assert.Nil(t, g.UtilizationPercent)
	assert.Contains(t, g.Unavailable["vram_used_bytes"], "値がありません")
	assert.Contains(t, g.Unavailable["utilization_percent"], "CSV に列")
	assert.Empty(t, g.Note)
}

func TestStatusWindowsCountersUsedBeyondTheDedicatedMemoryHasNoFree(t *testing.T) {
	mem := "\"Name\",\"DedicatedUsage\"\r\n\"luid_0x00000000_0x0000e9a2_phys_0\",\"900000000\"\r\n"
	r := &fakeRunner{out: map[string]string{psMemory: mem, psEngine: fixture(t, "wddm_engine.csv")}}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "windows", GOARCH: "amd64", Runner: r, Detector: fakeDetector{adapters: []gpu.Adapter{
		{Name: "Intel(R) UHD Graphics", VendorID: gpu.VendorIntel, VRAMBytes: 128 << 20, LUID: 0xe9a2},
	}}})
	g := st.GPUs[0]
	assert.EqualValues(t, 900000000, *g.VRAMUsedBytes)
	assert.Nil(t, g.VRAMFreeBytes)
	assert.Contains(t, g.Unavailable["vram_free_bytes"], "共有メモリ")
}

// An NVIDIA adapter whose nvidia-smi fails is read from the counters as well (and keeps nvidia-smi's reason for the temperature).
func TestStatusWindowsCountersCoverNvidiaWhenSmiFails(t *testing.T) {
	r := &fakeRunner{
		out: map[string]string{psMemory: "\"Name\",\"DedicatedUsage\"\r\n\"luid_0x00000000_0x0000e8f1_phys_0\",\"1073741824\"\r\n", psEngine: fixture(t, "wddm_engine.csv")},
		err: map[string]error{"nvidia-smi": errors.New("exit status 9")},
	}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "windows", GOARCH: "amd64", Runner: r, Detector: fakeDetector{adapters: []gpu.Adapter{
		{Name: "NVIDIA GeForce RTX 3060", VendorID: gpu.VendorNVIDIA, VRAMBytes: 12 << 30, LUID: 0xe8f1},
	}}})
	g := st.GPUs[0]
	assert.EqualValues(t, int64(1)<<30, *g.VRAMUsedBytes)
	assert.Equal(t, 71, *g.UtilizationPercent)
	assert.Contains(t, g.Unavailable["temperature_celsius"], "exit status 9")
}

// Off Windows, or for an adapter whose LUID is unknown, no command is run.
func TestStatusWindowsCountersAreNotAskedWithoutALUID(t *testing.T) {
	r := &fakeRunner{}
	gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "windows", GOARCH: "amd64", Runner: r, Detector: fakeDetector{adapters: []gpu.Adapter{
		{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 << 30},
	}}})
	for _, c := range r.calls {
		assert.NotContains(t, c, "powershell")
	}
	linux := &fakeRunner{}
	gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "linux", GOARCH: "amd64", Runner: linux, Detector: windowsAdapters()})
	for _, c := range linux.calls {
		assert.NotContains(t, c, "powershell")
	}
}
