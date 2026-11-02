package modelmanager

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
)

// The memory of the GPU is how this PC is built, and it hardly changes while mam runs, but finding it starts a child process (DXGI,
// nvidia-smi: up to 5 seconds). The model list draws it on every poll and every detail, so it is detected once per gpuVRAMTTL. A
// detection that failed is asked again sooner.
const (
	gpuVRAMTTL    = 5 * time.Minute
	gpuVRAMErrTTL = 30 * time.Second
)

// vramMemo is the last detection of the GPU memory and when it is stale.
type vramMemo struct {
	mu    sync.Mutex
	bytes uint64
	until time.Time
}

// GPUVRAM is the dedicated video memory in bytes of the largest GPU of this PC (0 when none is known), for judging the VRAM guideline of
// a model. It is the build of the PC, remembered for a few minutes; DetectGPU (the detect_gpu tool, the engine) is never remembered.
// Callers that wait on a detection in flight share its answer instead of starting their own.
func (s *Service) GPUVRAM(ctx context.Context) uint64 {
	m := &s.vram
	m.mu.Lock()
	defer m.mu.Unlock()
	now := s.Now()
	if now.Before(m.until) {
		return m.bytes
	}
	rep := s.GPU(ctx)
	bytes := hfplan.VRAMOf(rep.Adapters)
	switch {
	case ctx.Err() != nil:
		// A cancelled detection says nothing about the GPU: not remembered.
	case rep.Error != "":
		m.bytes, m.until = bytes, now.Add(gpuVRAMErrTTL)
	default:
		m.bytes, m.until = bytes, now.Add(gpuVRAMTTL)
	}
	return bytes
}
