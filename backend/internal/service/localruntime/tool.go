package localruntime

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
)

// A job that is not the server but runs a script of mam's own in the package of a runtime (the fine-tuning of GPT-SoVITS, package
// service/voicetrain). The runtime does not know what the job does: it hands out the launch (Tool), reserves the runtime while the job
// holds it (no install, removal or server start under it) and tells the machine's GPU.

// Tool is what BeginTool gives the job that holds the runtime.
type Tool struct {
	s    *Service
	inst *instance
	// Dir is the working directory for the job's files, below the runtime's own directory ("<runtime>/train"): the job makes its own
	// subdirectory in it and removes it when it ends.
	Dir string
	// GPU is the adapter the runtime would use: the NVIDIA one with the most video memory ("" and 0 when none could be listed).
	GPUName  string
	VRAMGB   int
	released bool
}

// BeginTool reserves the runtime id for a script of mam's own: the refusals are those of CheckStart (a machine that cannot run it, not
// installed, an install running, weights not there) and, for the weights that only the job needs (assets, by their names in the catalog), a
// *WeightsError; a runtime that a job holds already is ErrTraining. The server of the runtime is stopped (a fine-tuning uses the GPU and the
// two never run at the same moment). The caller ends the reservation with Tool.End.
func (s *Service) BeginTool(ctx context.Context, id string, assets ...string) (*Tool, error) {
	if err := s.CheckStart(ctx, id); err != nil {
		return nil, err
	}
	inst, _ := s.instance(id)
	if missing := inst.install.NotReadyNamed(assets...); len(missing) > 0 {
		return nil, &WeightsError{Runtime: id, Name: inst.rt.Name, Assets: missing}
	}
	if !inst.tool.CompareAndSwap(false, true) {
		return nil, ErrTraining
	}
	if err := inst.proc.Stop(); err != nil {
		inst.tool.Store(false)
		return nil, fmt.Errorf("stop %s before the job: %w", inst.rt.Name, err)
	}
	t := &Tool{s: s, inst: inst, Dir: filepath.Join(inst.layout.Root, "train")}
	t.GPUName, t.VRAMGB = bestGPU(s.report(ctx, true))
	return t, nil
}

// Spec is the launch of the tool script with args and env added (rtm.Installer.Tool); the caller sets the LogPath it wants.
func (t *Tool) Spec(script string, args, env []string) (proc.Spec, error) {
	return t.inst.install.Tool(script, args, env)
}

// End gives the runtime back; calling it again does nothing.
func (t *Tool) End() {
	if !t.released {
		t.released = true
		t.inst.tool.Store(false)
	}
}

// Holding reports whether a job holds the runtime (for the screen).
func (s *Service) Holding(id string) bool {
	inst, err := s.instance(id)
	return err == nil && inst.tool.Load()
}

func bestGPU(r gpu.Report) (string, int) {
	var best *gpu.Adapter
	for i := range r.Adapters {
		a := &r.Adapters[i]
		if a.VendorID == gpu.VendorNVIDIA && !a.Software && (best == nil || a.VRAMBytes > best.VRAMBytes) {
			best = a
		}
	}
	if best == nil {
		return "", 0
	}
	return best.Name, int(best.VRAMBytes >> 30)
}
