// Package vramcache remembers the dedicated video memory of this PC's GPU for the judgement of a model's VRAM guideline.
//
// The memory is how the PC is built and hardly changes while mam runs, but finding it starts a child process (DXGI, nvidia-smi: up to
// 5 seconds), and the model list draws the judgement on every poll and every detail. The state of the GPU that is asked for by name
// (detect_gpu, get_gpu_status) is never read through here.
package vramcache

import (
	"context"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/platform"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
)

const (
	// TTL is how long a detection is remembered.
	TTL = 5 * time.Minute
	// ErrTTL is how long one that failed is: a driver that came up is seen soon, and a failure is not waited for at every poll.
	ErrTTL = 30 * time.Second
)

// Profile is what a model is judged against: the video memory of the largest GPU in bytes (0 when none is known) and, for the OS / GPU generation
// columns of the catalog (Issue #979), the OS and the generation of that GPU (platform.Host; "" when not known).
type Profile struct {
	VRAM uint64
	Host platform.Host
}

// Cache is the last detection and when it is stale. The zero value is ready.
type Cache struct {
	mu      sync.Mutex
	profile Profile
	until   time.Time
}

// Get is the video memory in bytes of the largest GPU (0 when none is known); see Profile.
func (c *Cache) Get(ctx context.Context, now time.Time, detect func(context.Context) gpu.Report) uint64 {
	return c.Profile(ctx, now, detect).VRAM
}

// Profile is the remembered detection while it is fresh at now, else the one detect finds. A detection that a cancelled ctx cut short is not
// remembered. Callers that meet a detection in flight wait for it and share its answer instead of starting their own.
func (c *Cache) Profile(ctx context.Context, now time.Time, detect func(context.Context) gpu.Report) Profile {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Before(c.until) {
		return c.profile
	}
	rep := detect(ctx)
	p := Profile{VRAM: hfplan.VRAMOf(rep.Adapters), Host: platform.Host{OS: rep.OS, GPUArch: gpu.HostArch(rep)}}
	switch {
	case ctx.Err() != nil:
	case rep.Error != "":
		c.profile, c.until = p, now.Add(ErrTTL)
	default:
		c.profile, c.until = p, now.Add(TTL)
	}
	return p
}
