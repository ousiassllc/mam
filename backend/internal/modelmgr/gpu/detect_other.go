//go:build !windows

package gpu

import (
	"context"
	"os/exec"
	"runtime"
)

// System is the Detector of this OS: the GPU of an Apple silicon Mac on macOS (sysctl; no nvidia-smi), none elsewhere. The bundled
// engine is a Windows and macOS feature (spec/architecture/model-manager/model-manager.md §1); on another OS the user points mam at an
// external ComfyUI.
func System() (d Detector, smiPaths []string) {
	if runtime.GOOS == "darwin" {
		return Apple{Runner: ExecRunner{}}, nil
	}
	return None{}, nil
}

// None lists no adapter.
type None struct{}

// Adapters implements Detector.
func (None) Adapters(context.Context) ([]Adapter, error) { return nil, nil }

func hideWindow(*exec.Cmd) {}
