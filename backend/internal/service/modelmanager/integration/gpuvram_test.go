package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

const sixteenGiB = 16 << 30

// The memory of the GPU is asked for by every draw of the model list (every poll, every detail), and finding it starts a child process:
// it is detected once and remembered for a few minutes, then asked again. DetectGPU (the detect_gpu tool) is never remembered.
func TestGPUVRAMIsRememberedForAWhileButDetectGPUIsNot(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	r := mmtest.New(t, mmtest.WithClock(func() time.Time { return now }))
	r.GPUReport.Adapters = []gpu.Adapter{{Name: "RTX", VRAMBytes: sixteenGiB}}
	ctx := context.Background()

	assert.Equal(t, uint64(sixteenGiB), r.Svc.GPUVRAM(ctx))
	assert.Equal(t, uint64(sixteenGiB), r.Svc.GPUVRAM(ctx))
	assert.EqualValues(t, 1, r.GPUDetections.Load(), "the second draw of the list runs no child process")

	r.GPUReport.Adapters = []gpu.Adapter{{Name: "RTX", VRAMBytes: 24 << 30}}
	assert.Equal(t, uint64(24<<30), r.Svc.DetectGPU(ctx).Adapters[0].VRAMBytes, "the state asked for by name is read afresh")
	assert.EqualValues(t, 2, r.GPUDetections.Load())
	assert.Equal(t, uint64(sixteenGiB), r.Svc.GPUVRAM(ctx), "and it does not touch what the list remembers")

	now = now.Add(4 * time.Minute)
	assert.Equal(t, uint64(sixteenGiB), r.Svc.GPUVRAM(ctx), "still within the time")
	now = now.Add(2 * time.Minute)
	assert.Equal(t, uint64(24<<30), r.Svc.GPUVRAM(ctx), "after the time the GPU is looked at again")
	assert.EqualValues(t, 3, r.GPUDetections.Load())
}

// A detection that failed, or that a cancelled request cut short, says nothing lasting about the GPU.
func TestGPUVRAMDoesNotKeepAFailedDetectionForLong(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	r := mmtest.New(t, mmtest.WithClock(func() time.Time { return now }))
	r.GPUReport = gpu.Report{Error: "nvidia-smi: timed out"}
	ctx := context.Background()

	assert.Zero(t, r.Svc.GPUVRAM(ctx))
	assert.Zero(t, r.Svc.GPUVRAM(ctx))
	assert.EqualValues(t, 1, r.GPUDetections.Load(), "a failure is not asked again at once (every poll would wait for the timeout)")

	r.GPUReport = gpu.Report{Adapters: []gpu.Adapter{{Name: "RTX", VRAMBytes: sixteenGiB}}}
	now = now.Add(time.Minute)
	assert.Equal(t, uint64(sixteenGiB), r.Svc.GPUVRAM(ctx), "but soon, so that a driver that came up is seen")

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	now = now.Add(time.Hour)
	r.Svc.GPUVRAM(cancelled)
	r.GPUReport = gpu.Report{Adapters: []gpu.Adapter{{Name: "RTX", VRAMBytes: 8 << 30}}}
	assert.Equal(t, uint64(8<<30), r.Svc.GPUVRAM(ctx), "a cancelled detection was not remembered")
}
