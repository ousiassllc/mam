// Package gpuvramtest checks that the video memory of the GPU, which the model list judges a model's VRAM guideline against, is not
// detected again (a child process: DXGI, nvidia-smi) at every draw of the list.
package gpuvramtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

const sixteenGiB = 16 << 30

func TestMain(m *testing.M) { mmtest.Main(m) }

// The memory of the GPU is asked for by every draw of the model list (every poll, every detail), and finding it starts a child process:
// it is detected once and remembered for a few minutes, then asked again. DetectGPU (the detect_gpu tool) is never remembered.
func TestGPUVRAMIsRememberedForAWhileButDetectGPUIsNot(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	r := mmtest.New(t, mmtest.WithClock(func() time.Time { return now }))
	r.GPUReport.Adapters = []gpu.Adapter{{Name: "RTX", VRAMBytes: sixteenGiB}}
	ctx := context.Background()

	assert.Equal(t, uint64(sixteenGiB), mmview.GPUVRAM(ctx, r.Svc))
	assert.Equal(t, uint64(sixteenGiB), mmview.GPUVRAM(ctx, r.Svc))
	assert.EqualValues(t, 1, r.GPUDetections.Load(), "the second draw of the list runs no child process")

	r.GPUReport.Adapters = []gpu.Adapter{{Name: "RTX", VRAMBytes: 24 << 30}}
	assert.Equal(t, uint64(24<<30), r.Svc.DetectGPU(ctx).Adapters[0].VRAMBytes, "the state asked for by name is read afresh")
	assert.EqualValues(t, 2, r.GPUDetections.Load())
	assert.Equal(t, uint64(sixteenGiB), mmview.GPUVRAM(ctx, r.Svc), "and it does not touch what the list remembers")

	now = now.Add(4 * time.Minute)
	assert.Equal(t, uint64(sixteenGiB), mmview.GPUVRAM(ctx, r.Svc), "still within the time")
	now = now.Add(2 * time.Minute)
	assert.Equal(t, uint64(24<<30), mmview.GPUVRAM(ctx, r.Svc), "after the time the GPU is looked at again")
	assert.EqualValues(t, 3, r.GPUDetections.Load())
}

// A detection that failed, or that a cancelled request cut short, says nothing lasting about the GPU.
func TestGPUVRAMDoesNotKeepAFailedDetectionForLong(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	r := mmtest.New(t, mmtest.WithClock(func() time.Time { return now }))
	r.GPUReport = gpu.Report{Error: "nvidia-smi: timed out"}
	ctx := context.Background()

	assert.Zero(t, mmview.GPUVRAM(ctx, r.Svc))
	assert.Zero(t, mmview.GPUVRAM(ctx, r.Svc))
	assert.EqualValues(t, 1, r.GPUDetections.Load(), "a failure is not asked again at once (every poll would wait for the timeout)")

	r.GPUReport = gpu.Report{Adapters: []gpu.Adapter{{Name: "RTX", VRAMBytes: sixteenGiB}}}
	now = now.Add(time.Minute)
	assert.Equal(t, uint64(sixteenGiB), mmview.GPUVRAM(ctx, r.Svc), "but soon, so that a driver that came up is seen")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	now = now.Add(time.Hour)
	mmview.GPUVRAM(cancelled, r.Svc)
	r.GPUReport = gpu.Report{Adapters: []gpu.Adapter{{Name: "RTX", VRAMBytes: 8 << 30}}}
	assert.Equal(t, uint64(8<<30), mmview.GPUVRAM(ctx, r.Svc), "a cancelled detection was not remembered")
}

// The OS and the GPU generation for the catalog's columns (Issue #979) come from the same remembered detection: no child process more, and a
// new one after the time.
func TestHostIsRememberedWithTheVRAM(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	r := mmtest.New(t, mmtest.WithClock(func() time.Time { return now }))
	r.GPUReport.OS = "windows"
	r.GPUReport.Adapters = []gpu.Adapter{{Name: "NVIDIA GeForce RTX 5090", VendorID: gpu.VendorNVIDIA, VRAMBytes: 32 << 30, ComputeCap: 12}}
	ctx := context.Background()

	assert.Equal(t, platform.Host{OS: "windows", GPUArch: platform.ArchBlackwell}, mmview.Host(ctx, r.Svc))
	assert.Equal(t, uint64(32<<30), mmview.GPUVRAM(ctx, r.Svc))
	assert.EqualValues(t, 1, r.GPUDetections.Load(), "the memory and the generation are one detection")

	r.GPUReport.OS, r.GPUReport.Adapters = "darwin", nil
	assert.Equal(t, platform.ArchBlackwell, mmview.Host(ctx, r.Svc).GPUArch, "still remembered")
	now = now.Add(6 * time.Minute)
	assert.Equal(t, platform.Host{OS: "darwin", GPUArch: platform.ArchApple}, mmview.Host(ctx, r.Svc), "after the time the PC is looked at again")
}
