package modelmanager

import (
	"context"
	"os"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/detect"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// systemDetect looks for a ComfyUI on this machine: its usual ports and the ComfyUI Desktop configuration.
func systemDetect(ctx context.Context) detect.Report {
	dir, _ := os.UserConfigDir()
	return detect.Detect(ctx, detect.Options{ConfigDir: dir})
}

// DetectGPU lists the GPUs of the machine and recommends the engine build for them (nothing when there is no
// supported GPU: the screen then offers an external URL, or the CPU on an explicit choice).
func (s *Service) DetectGPU(ctx context.Context) gpu.Report { return s.GPU(ctx) }

// DetectComfyUI finds a ComfyUI that is already running on this machine. One that is found is used as an external
// endpoint (a ComfyUI Desktop in particular): mam never starts or stops it.
func (s *Service) DetectComfyUI(ctx context.Context) detect.Report { return s.Comfy(ctx) }
