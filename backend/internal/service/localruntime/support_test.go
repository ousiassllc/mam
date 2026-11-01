package localruntime_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// Every machine that cannot run the runtime is refused by Install, Start and Prepare with the same sentence that Status shows,
// and nothing is created next to mam.exe.
func TestAMachineThatCannotRunTheRuntimeIsRefusedWithTheReasonAndTheCloudFallback(t *testing.T) {
	amd := gpu.Adapter{Name: "AMD Radeon RX 7900 XTX", VendorID: gpu.VendorAMD, VRAMBytes: 24 * gib}
	intel := gpu.Adapter{Name: "Intel(R) Arc(TM) A770", VendorID: gpu.VendorIntel, VRAMBytes: 16 * gib}
	for name, tc := range map[string]struct {
		opts   []option
		code   rtm.Code
		reason string
	}{
		"macOS":        {[]option{platform("darwin", "arm64"), withGPU()}, rtm.CodeOS, "macOS では GPT-SoVITS の自動導入に対応していません"},
		"Linux":        {[]option{platform("linux", "amd64"), withGPU(rtx("NVIDIA GeForce RTX 4090", 24, 8.9))}, rtm.CodeOS, "Linux では GPT-SoVITS の自動導入に対応していません"},
		"Windows ARM":  {[]option{platform("windows", "arm64")}, rtm.CodeArch, "64 ビットの x86 以外"},
		"no GPU found": {[]option{withGPU()}, rtm.CodeNoGPU, "GPU を取得できなかった"},
		"AMD":          {[]option{withGPU(amd)}, rtm.CodeNotNVIDIA, "AMD・Intel の GPU では使えません"},
		"Intel":        {[]option{withGPU(intel)}, rtm.CodeNotNVIDIA, "AMD・Intel の GPU では使えません"},
		"low VRAM":     {[]option{withGPU(rtx("NVIDIA GeForce GT 1030", 2, 6.1))}, rtm.CodeLowVRAM, "NVIDIA GeForce GT 1030 のビデオメモリは 2.0 GB で、GPT-SoVITS に必要な 4 GB に足りない"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, tc.opts...)
			assertRefused := func(err error) {
				t.Helper()
				var se *localruntime.SupportError
				require.ErrorAs(t, err, &se)
				require.ErrorIs(t, err, localruntime.ErrUnsupported)
				assert.Equal(t, tc.code, se.Support.Code)
				assert.Equal(t, "gpt-sovits", se.Runtime)
				assert.Contains(t, err.Error(), tc.reason)
				assert.Contains(t, err.Error(), "ElevenLabs", "what to use instead")
			}
			ref, err := r.svc.Install(ctx(), "gpt-sovits")
			assertRefused(err)
			assert.Empty(t, ref.JobID, "no job was made")
			_, err = r.svc.Start(ctx(), "gpt-sovits")
			assertRefused(err)
			assertRefused(r.svc.Prepare(ctx(), "gpt-sovits"))

			v, err := r.svc.Status(ctx(), "gpt-sovits")
			require.NoError(t, err)
			assert.False(t, v.Support.Supported)
			assert.Equal(t, tc.code, v.Support.Code)
			assert.Contains(t, v.Support.Reason, tc.reason)
			assert.Contains(t, v.Support.Fallback, "ElevenLabs")
			assert.False(t, v.Writable, "the place is not probed on a machine that cannot use it")
			assert.NoDirExists(t, r.root, "nothing was created next to mam.exe")
			assert.Empty(t, r.host.Ranges(), "nothing was requested")
		})
	}
}

// A runtime that was installed when the machine could run it is not started once it cannot (the GPU was taken out, the disk moved).
func TestAnInstalledRuntimeIsNotStartedWhenTheMachineNoLongerQualifies(t *testing.T) {
	r := newRig(t)
	r.install()
	r.gpu = gpu.Report{Adapters: []gpu.Adapter{rtx("NVIDIA GeForce GT 1030", 2, 6.1)}}
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	var se *localruntime.SupportError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, rtm.CodeLowVRAM, se.Support.Code)
	v, _ := r.svc.Status(ctx(), "gpt-sovits")
	assert.True(t, v.Installed, "the files stay; Remove deletes them")
	assert.Equal(t, "stopped", string(v.Process.State))
}

// The real catalog on this (Linux CI) host: GPT-SoVITS and MuseTalk are refused, each with its own sentence of the catalog and the cloud
// provider that makes the same thing.
func TestTheRealCatalogRefusesThisHostUnlessItIsWindows(t *testing.T) {
	svc, err := localruntime.New(localruntime.Deps{Root: t.TempDir(), Platform: rtm.Platform{OS: "darwin", Arch: "arm64"}, Queue: newRig(t).queue})
	require.NoError(t, err)
	list := svc.List(ctx())
	require.Len(t, list, 2)
	assert.Equal(t, "gpt-sovits", list[0].ID)
	assert.False(t, list[0].Support.Supported)
	assert.Contains(t, list[0].Support.Message(), "macOS では GPT-SoVITS の自動導入に対応していません")
	assert.Contains(t, list[0].Support.Message(), "ElevenLabs")
	assert.Equal(t, "musetalk", list[1].ID)
	assert.False(t, list[1].Support.Supported)
	assert.Contains(t, list[1].Support.Message(), "macOS では MuseTalk 1.5 を自動導入できません")
	assert.Contains(t, list[1].Support.Message(), "Sync Labs")
}

// The screen polls Status; a GPU detection runs nvidia-smi, so Status reuses a young report while Install and Start ask again.
func TestStatusReusesTheGPUReportAndInstallAndStartDetectAgain(t *testing.T) {
	var calls atomic.Int32
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	r := newRig(t, func(rg *rig, d *localruntime.Deps) {
		d.GPU = func(context.Context) gpu.Report { calls.Add(1); return rg.gpu }
		d.Now = func() time.Time { return now }
	})
	for range 3 {
		_, err := r.svc.Status(ctx(), "gpt-sovits")
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, calls.Load())

	now = now.Add(2 * time.Minute)
	r.svc.List(ctx())
	assert.EqualValues(t, 2, calls.Load(), "an old report is detected again")

	r.install()
	assert.EqualValues(t, 4, calls.Load(), "Install detects again, and so does the job when it runs")
	_, err := r.svc.Start(ctx(), "gpt-sovits")
	require.NoError(t, err)
	assert.EqualValues(t, 5, calls.Load())
}
