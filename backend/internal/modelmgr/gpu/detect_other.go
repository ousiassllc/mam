//go:build !windows

package gpu

import (
	"context"
	"os/exec"
)

// System is the Detector of this OS: none. The bundled engine is a Windows feature (spec/architecture/
// spec/architecture/model-manager/model-manager.md §1); elsewhere the user points mam at an external ComfyUI.
func System() (d Detector, smiPaths []string) { return None{}, nil }

// None lists no adapter.
type None struct{}

// Adapters implements Detector.
func (None) Adapters(context.Context) ([]Adapter, error) { return nil, nil }

func hideWindow(*exec.Cmd) {}
