//go:build windows

package gpu

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// System is the Detector of Windows (DXGI) and the places nvidia-smi lives: the PATH (the DCH driver puts it in
// System32), then the NVSMI folder of the standard driver.
func System() (d Detector, smiPaths []string) {
	paths := []string{"nvidia-smi"}
	if pf := os.Getenv("ProgramW6432"); pf != "" {
		paths = append(paths, filepath.Join(pf, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"))
	}
	return DXGI{}, paths
}
