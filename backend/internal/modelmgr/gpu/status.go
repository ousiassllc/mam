package gpu

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// The live state of the GPUs (get_gpu_status). Detect says which GPU there is and which build to install; this says
// how busy it is now. Every command goes through a Runner, so that the parsing and the choice of source are
// tested on Linux with fakes: nvidia-smi for NVIDIA (Windows, Linux), ioreg + sysctl + vm_stat for Apple Silicon
// (no administrator rights needed, unlike powermetrics). A value that cannot be read is never an error and never
// null: it is left out and its reason is in Unavailable (by the JSON name of the field).

// Runner runs a command and returns its standard output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs real commands: never a console window, and each gives up after 5 seconds.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed program names and arguments
	hideWindow(cmd)
	return cmd.Output()
}

// Process owners.
const (
	OwnerMam    = "mam"
	OwnerEngine = "mam_engine" // the ComfyUI that mam started
	OwnerOther  = "other"
)

// GPU is one GPU and what could be read of its state now. Sizes are bytes, a percentage is 0-100.
type GPU struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Vendor string `json:"vendor,omitempty"`
	// Memory is "dedicated" (video memory of its own) or "unified" (Apple Silicon: shared with the system).
	Memory             string `json:"memory"`
	VRAMTotalBytes     *int64 `json:"vram_total_bytes,omitempty"`
	VRAMUsedBytes      *int64 `json:"vram_used_bytes,omitempty"`
	VRAMFreeBytes      *int64 `json:"vram_free_bytes,omitempty"`
	UtilizationPercent *int   `json:"utilization_percent,omitempty"`
	TemperatureCelsius *int   `json:"temperature_celsius,omitempty"`
	// Note explains how to read the values of this GPU.
	Note string `json:"note,omitempty"`
	// Unavailable is why a field is missing, by its JSON name.
	Unavailable map[string]string `json:"unavailable,omitempty"`
}

// Process is a process that computes on a GPU (nvidia-smi --query-compute-apps).
type Process struct {
	PID        int    `json:"pid"`
	Name       string `json:"name"`
	GPUIndex   *int   `json:"gpu_index,omitempty"`
	Owner      string `json:"owner" jsonschema:"mam（mam 自身）・mam_engine（mam が起動した ComfyUI）・other"`
	UsedBytes  *int64 `json:"used_bytes,omitempty"`
	UsedReason string `json:"used_bytes_unavailable,omitempty"`
}

// Status is the state of the GPUs of this machine now.
type Status struct {
	OS string `json:"os"`
	// Source is where the numbers came from: nvidia-smi, ioreg (Apple Silicon), adapters (the adapter list
	// only) or none.
	Source    string    `json:"source"`
	GPUs      []GPU     `json:"gpus"`
	Processes []Process `json:"processes"`
	// Unavailable is why a part is missing: "gpus" (no GPU could be read) or "processes".
	Unavailable map[string]string `json:"unavailable,omitempty"`
}

// StatusOptions are what ReadStatus uses. Runner is required.
type StatusOptions struct {
	GOOS, GOARCH string
	Runner       Runner
	// Detector lists the adapters that nvidia-smi does not cover (AMD, Intel) or a machine whose nvidia-smi fails (nil: none).
	Detector Detector
	// SmiPaths are the places nvidia-smi may be, in order (default "nvidia-smi").
	SmiPaths []string
	// MamPID is the process ID of mam itself; EngineDir is the folder of the ComfyUI engine mam installs (a process
	// whose program is below it is OwnerEngine, whatever its PID: the engine is a python.exe with children). Any other is OwnerOther.
	MamPID    int
	EngineDir string
}

// owner says whose a process is.
func (o StatusOptions) owner(pid int, program string) string {
	switch {
	case o.MamPID != 0 && pid == o.MamPID:
		return OwnerMam
	case underDir(program, o.EngineDir):
		return OwnerEngine
	}
	return OwnerOther
}

// underDir reports whether program is a file below dir; the comparison ignores case and the kind of slash (Windows).
func underDir(program, dir string) bool {
	if dir == "" {
		return false
	}
	norm := func(s string) string {
		return strings.TrimRight(strings.ToLower(strings.ReplaceAll(s, `\`, "/")), "/") + "/"
	}
	return strings.HasPrefix(norm(program), norm(dir))
}

// StatusSystem is ReadStatus for this machine.
func StatusSystem(ctx context.Context, mamPID int, engineDir string) Status {
	d, smi := System()
	return ReadStatus(ctx, StatusOptions{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Runner: ExecRunner{}, Detector: d, SmiPaths: smi, MamPID: mamPID, EngineDir: engineDir})
}

// ReadStatus reads the state of the GPUs.
func ReadStatus(ctx context.Context, o StatusOptions) Status {
	st := Status{OS: o.GOOS, Source: "none", GPUs: []GPU{}, Processes: []Process{}, Unavailable: map[string]string{}}
	switch {
	case o.GOOS == "darwin" && o.GOARCH == "arm64":
		st.readApple(ctx, o)
	case o.GOOS == "darwin":
		st.Unavailable["gpus"] = "Apple Silicon ではない Mac の GPU の状態は取得しません（対応しているのは Apple Silicon の Mac です）"
	default:
		st.readNvidia(ctx, o)
	}
	if len(st.Unavailable) == 0 {
		st.Unavailable = nil
	}
	return st
}
