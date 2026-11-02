package gpu_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// fakeRunner answers a command by its name and arguments joined with a space; an unknown command is "not found".
type fakeRunner struct {
	out   map[string]string
	err   map[string]error
	calls []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, key)
	for prefix, err := range f.err {
		if strings.HasPrefix(key, prefix) {
			return nil, err
		}
	}
	for prefix, out := range f.out {
		if strings.HasPrefix(key, prefix) {
			return []byte(out), nil
		}
	}
	return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
}

type fakeDetector struct {
	adapters []gpu.Adapter
	err      error
}

func (d fakeDetector) Adapters(context.Context) ([]gpu.Adapter, error) { return d.adapters, d.err }

const smiGPUs = "nvidia-smi --query-gpu=index,uuid,name,memory.total,memory.used,memory.free,utilization.gpu,temperature.gpu"
const smiApps = "nvidia-smi --query-compute-apps=gpu_uuid,pid,process_name,used_memory"

func TestStatusNVIDIA(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		smiGPUs: "0, GPU-aaa, NVIDIA GeForce RTX 4070 Ti SUPER, 16376, 9000, 7000, 87, 71\n",
		smiApps: "GPU-aaa, 4242, C:\\ComfyUI\\python_embedded\\python.exe, 8000\nGPU-aaa, 77, C:\\Other\\app.exe, [N/A]\nGPU-aaa, 4243, mam.exe, 0\n",
	}}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{
		GOOS: "windows", GOARCH: "amd64", Runner: r, SmiPaths: []string{"nvidia-smi"},
		Detector: fakeDetector{adapters: []gpu.Adapter{{Name: "NVIDIA GeForce RTX 4070 Ti SUPER", VendorID: gpu.VendorNVIDIA, VRAMBytes: 16 << 30}}},
		MamPID:   4243, EngineDir: `C:\ComfyUI`,
	})
	assert.Equal(t, "nvidia-smi", st.Source)
	assert.Empty(t, st.Unavailable)
	require.Len(t, st.GPUs, 1, "the adapter that nvidia-smi covers is not listed twice")
	g := st.GPUs[0]
	assert.Equal(t, "NVIDIA GeForce RTX 4070 Ti SUPER", g.Name)
	assert.Equal(t, "dedicated", g.Memory)
	assert.EqualValues(t, int64(16376)<<20, *g.VRAMTotalBytes)
	assert.EqualValues(t, int64(9000)<<20, *g.VRAMUsedBytes)
	assert.EqualValues(t, int64(7000)<<20, *g.VRAMFreeBytes)
	assert.Equal(t, 87, *g.UtilizationPercent)
	assert.Equal(t, 71, *g.TemperatureCelsius)
	assert.Empty(t, g.Unavailable)

	require.Len(t, st.Processes, 3)
	assert.Equal(t, gpu.OwnerEngine, st.Processes[0].Owner)
	assert.EqualValues(t, int64(8000)<<20, *st.Processes[0].UsedBytes)
	assert.Equal(t, 0, *st.Processes[0].GPUIndex)
	assert.Equal(t, gpu.OwnerOther, st.Processes[1].Owner)
	assert.Nil(t, st.Processes[1].UsedBytes)
	assert.Contains(t, st.Processes[1].UsedReason, "WDDM")
	assert.Equal(t, gpu.OwnerMam, st.Processes[2].Owner)
}

// A field the driver does not support is left out with its reason; the others stay.
func TestStatusNVIDIAFieldsNotSupported(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		smiGPUs: "0, GPU-aaa, NVIDIA RTX, A4000, 16376, 100, 16276, [N/A], [Not Supported]\n",
		smiApps: "",
	}}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "linux", GOARCH: "amd64", Runner: r})
	require.Len(t, st.GPUs, 1)
	g := st.GPUs[0]
	assert.Equal(t, "NVIDIA RTX, A4000", g.Name, "a comma in the name is kept")
	assert.NotNil(t, g.VRAMFreeBytes)
	assert.Nil(t, g.UtilizationPercent)
	assert.Nil(t, g.TemperatureCelsius)
	assert.Contains(t, g.Unavailable["utilization_percent"], "[N/A]")
	assert.Contains(t, g.Unavailable["temperature_celsius"], "[Not Supported]")
	assert.Empty(t, st.Processes)
	assert.Empty(t, st.Unavailable)
}

// nvidia-smi is missing (an AMD machine): the adapters are listed with their size, and every live value has its reason.
func TestStatusWithoutNvidiaSmiFallsBackToAdapters(t *testing.T) {
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{
		GOOS: "windows", GOARCH: "amd64", Runner: &fakeRunner{}, SmiPaths: []string{"nvidia-smi", `C:\NVSMI\nvidia-smi.exe`},
		Detector: fakeDetector{adapters: []gpu.Adapter{
			{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 << 30},
			{Name: "Microsoft Basic Render Driver", VendorID: 0x1414, Software: true},
		}},
	})
	assert.Equal(t, "adapters", st.Source)
	require.Len(t, st.GPUs, 1, "a software adapter is not a GPU")
	g := st.GPUs[0]
	assert.Equal(t, "AMD", g.Vendor)
	assert.EqualValues(t, int64(24)<<30, *g.VRAMTotalBytes)
	assert.Nil(t, g.VRAMUsedBytes)
	for _, f := range []string{"vram_used_bytes", "vram_free_bytes", "utilization_percent", "temperature_celsius"} {
		assert.Contains(t, g.Unavailable[f], "NVIDIA 以外", f)
	}
	assert.Contains(t, st.Unavailable["processes"], "nvidia-smi が見つかりません")
	assert.NotContains(t, st.Unavailable, "gpus")
}

// An NVIDIA adapter whose nvidia-smi fails is listed by the adapter list, with the failure as the reason.
func TestStatusNvidiaSmiFailsForNvidiaAdapter(t *testing.T) {
	r := &fakeRunner{err: map[string]error{"nvidia-smi": errors.New("exit status 9")}}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{
		GOOS: "windows", GOARCH: "amd64", Runner: r,
		Detector: fakeDetector{adapters: []gpu.Adapter{{Name: "NVIDIA GeForce RTX 3060", VendorID: gpu.VendorNVIDIA, VRAMBytes: 12 << 30}}},
	})
	require.Len(t, st.GPUs, 1)
	assert.Contains(t, st.GPUs[0].Unavailable["utilization_percent"], "exit status 9")
	assert.NotNil(t, st.GPUs[0].VRAMTotalBytes)
}

// Nothing can be read: not an error, the reason is in Unavailable.
func TestStatusNothingAvailable(t *testing.T) {
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "linux", GOARCH: "amd64", Runner: &fakeRunner{}, Detector: fakeDetector{}})
	assert.Equal(t, "none", st.Source)
	assert.NotNil(t, st.GPUs)
	assert.Empty(t, st.GPUs)
	assert.Contains(t, st.Unavailable["gpus"], "nvidia-smi が見つかりません")
	assert.NotNil(t, st.Processes)
}

func TestStatusAdapterListFails(t *testing.T) {
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "windows", GOARCH: "amd64", Runner: &fakeRunner{},
		Detector: fakeDetector{err: errors.New("dxgi: boom")}})
	assert.Contains(t, st.Unavailable["adapters"], "dxgi: boom")
	assert.Contains(t, st.Unavailable, "gpus")
}

const (
	ioregOut = `+-o AGXAcceleratorG14X  <class AGXAcceleratorG14X, id 0x100000a9d, registered, matched, active, busy 0 (3 ms), retain 28>
    {
      "IOClass" = "AGXAcceleratorG14X"
      "PerformanceStatistics" = {"In use system memory (driver)"=0,"Alloc system memory"=2147483648,"Tiler Utilization %"=4,"Device Utilization %"=37,"In use system memory"=1073741824,"Renderer Utilization %"=36}
    }
`
	vmStatOut = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               10000.
Pages active:                            500000.
Pages inactive:                           20000.
Pages speculative:                         5000.
Pages wired down:                        100000.
`
)

func TestStatusAppleSilicon(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"sysctl -n machdep.cpu.brand_string": "Apple M2 Pro\n",
		"sysctl -n hw.memsize":               "17179869184\n",
		"ioreg":                              ioregOut,
		"vm_stat":                            vmStatOut,
	}}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "darwin", GOARCH: "arm64", Runner: r})
	assert.Equal(t, "ioreg", st.Source)
	require.Len(t, st.GPUs, 1)
	g := st.GPUs[0]
	assert.Equal(t, "Apple M2 Pro", g.Name)
	assert.Equal(t, "unified", g.Memory)
	assert.EqualValues(t, int64(16)<<30, *g.VRAMTotalBytes)
	assert.EqualValues(t, int64(1)<<30, *g.VRAMUsedBytes)
	assert.EqualValues(t, int64(35000)*16384, *g.VRAMFreeBytes)
	assert.Equal(t, 37, *g.UtilizationPercent)
	assert.Nil(t, g.TemperatureCelsius)
	assert.Contains(t, g.Unavailable["temperature_celsius"], "powermetrics")
	assert.Contains(t, g.Note, "ユニファイドメモリ")
	assert.Contains(t, st.Unavailable["processes"], "macOS")
	for _, c := range r.calls {
		assert.False(t, strings.HasPrefix(c, "powermetrics") || strings.HasPrefix(c, "sudo"), "no privileged command: %s", c)
	}
}

// The commands of macOS fail or print something else: each value gets its reason and the call still answers.
func TestStatusAppleSiliconPartlyUnavailable(t *testing.T) {
	r := &fakeRunner{
		out: map[string]string{"sysctl -n hw.memsize": "17179869184\n", "ioreg": "no accelerator\n", "vm_stat": "garbage"},
		err: map[string]error{"sysctl -n machdep": errors.New("denied")},
	}
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "darwin", GOARCH: "arm64", Runner: r})
	require.Len(t, st.GPUs, 1)
	g := st.GPUs[0]
	assert.Equal(t, "Apple GPU", g.Name)
	assert.NotNil(t, g.VRAMTotalBytes)
	assert.Nil(t, g.VRAMUsedBytes)
	assert.Nil(t, g.UtilizationPercent)
	assert.Nil(t, g.VRAMFreeBytes)
	for _, f := range []string{"vram_used_bytes", "utilization_percent", "vram_free_bytes"} {
		assert.NotEmpty(t, g.Unavailable[f], f)
	}

	none := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "darwin", GOARCH: "arm64", Runner: &fakeRunner{}})
	require.Len(t, none.GPUs, 1)
	assert.Nil(t, none.GPUs[0].VRAMTotalBytes)
	assert.Contains(t, none.GPUs[0].Unavailable["vram_total_bytes"], "hw.memsize")
	assert.Contains(t, none.GPUs[0].Unavailable["utilization_percent"], "ioreg")
}

func TestStatusIntelMac(t *testing.T) {
	st := gpu.ReadStatus(context.Background(), gpu.StatusOptions{GOOS: "darwin", GOARCH: "amd64", Runner: &fakeRunner{}})
	assert.Empty(t, st.GPUs)
	assert.Contains(t, st.Unavailable["gpus"], "Apple Silicon")
}
