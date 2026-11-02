// Package gpustate answers get_gpu_status: the live state of the GPUs (package gpu: nvidia-smi on NVIDIA, ioreg on Apple
// Silicon) together with what every ComfyUI endpoint mam knows says about its video memory and its queue. It reads and never
// changes anything; a part that cannot be read comes with its reason and is never an error.
package gpustate

import (
	"context"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
)

// comfyProbeTimeout bounds the questions put to one ComfyUI: the state is read to decide whether to start a job now, so
// a server that does not answer is reported as unreachable rather than waited for.
const comfyProbeTimeout = 4 * time.Second

// Deps are what Read needs.
type Deps struct {
	// Live reads the GPUs; mamPID and engineDir tell which processes are mam's (nil: gpu.StatusSystem).
	Live func(ctx context.Context, mamPID int, engineDir string) gpu.Status
	// Adapter lists the endpoints (nil: no ComfyUI is asked); Keys and StagingDir are what a client of an endpoint needs.
	Adapter    *modeladapter.Adapter
	Keys       credential.Store
	StagingDir string
	// EngineDir is the folder of the bundled engine; Bundled is the name of its endpoint.
	EngineDir, Bundled string
}

// View is the state of the GPUs now: the GPUs themselves (nvidia-smi / Apple Silicon), and for every ComfyUI
// mam knows (the bundled engine and the external endpoints) what it holds in video memory, what it is running and what
// waits. It reads and never changes anything.
type View struct {
	gpu.Status
	ComfyUI []ComfyUIState `json:"comfyui" jsonschema:"mam が知っている ComfyUI（同梱エンジンと外部の接続先）ごとの VRAM とジョブ"`
}

// ComfyUIState is what one ComfyUI endpoint says (GET /system_stats and GET /queue).
type ComfyUIState struct {
	Endpoint string `json:"endpoint"`
	URL      string `json:"url"`
	// Bundled is the engine mam started (and stops); the others are external servers.
	Bundled   bool   `json:"bundled"`
	Reachable bool   `json:"reachable"`
	Reason    string `json:"unreachable_reason,omitempty"`
	Version   string `json:"version,omitempty"`
	// Devices are the devices as ComfyUI sees them: its own use of the video memory is the PyTorch allocation.
	Devices []ComfyUIDevice `json:"devices"`
	// Running and Pending are the jobs of the queue, in the order the server runs them (Pending[0] is next).
	Running []QueueJob `json:"running"`
	Pending []QueueJob `json:"pending"`
	// Unavailable is why a part is missing: "devices" or "queue".
	Unavailable map[string]string `json:"unavailable,omitempty"`
}

// ComfyUIDevice is a device of /system_stats. Sizes are bytes.
type ComfyUIDevice struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty" jsonschema:"cuda・mps・cpu など"`
	// VRAMTotal and VRAMFree are what the device has (as ComfyUI sees it).
	VRAMTotal int64 `json:"vram_total_bytes"`
	VRAMFree  int64 `json:"vram_free_bytes"`
	// TorchVRAMTotal and TorchVRAMFree are PyTorch's pool inside it; ComfyUIUsed (total - free) is the memory ComfyUI itself
	// holds, models and cache included. It is not given when the server does not report the pool.
	TorchVRAMTotal int64  `json:"torch_vram_total_bytes,omitempty"`
	TorchVRAMFree  int64  `json:"torch_vram_free_bytes,omitempty"`
	ComfyUIUsed    *int64 `json:"comfyui_used_bytes,omitempty"`
	UsedReason     string `json:"comfyui_used_unavailable,omitempty"`
}

// QueueJob is a job in the queue of a ComfyUI.
type QueueJob struct {
	PromptID string `json:"prompt_id"`
	// Position is the place in the queue of a job that waits (1 is next); a running job has none.
	Position int        `json:"position,omitempty"`
	Progress *live.View `json:"progress,omitempty"`
	// ProgressUnavailable is why Progress is missing.
	ProgressUnavailable string `json:"progress_unavailable,omitempty"`
}

// Read reads the state of the GPUs and of every ComfyUI endpoint.
func Read(ctx context.Context, d Deps) View {
	read := d.Live
	if read == nil {
		read = gpu.StatusSystem
	}
	view := View{Status: read(ctx, os.Getpid(), d.EngineDir), ComfyUI: []ComfyUIState{}}
	if d.Adapter == nil {
		return view
	}
	endpoints := d.Adapter.Config().Endpoints
	names := make([]string, 0, len(endpoints))
	for name, ep := range endpoints {
		if ep.Kind == localcfg.KindComfyUI {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		view.ComfyUI = append(view.ComfyUI, d.comfyState(ctx, name, endpoints[name]))
	}
	return view
}

func (d Deps) comfyState(ctx context.Context, name string, ep localcfg.Endpoint) ComfyUIState {
	st := ComfyUIState{Endpoint: name, URL: ep.BaseURL, Bundled: name == d.Bundled, Devices: []ComfyUIDevice{}, Running: []QueueJob{}, Pending: []QueueJob{}, Unavailable: map[string]string{}}
	defer func() {
		if len(st.Unavailable) == 0 {
			st.Unavailable = nil
		}
	}()
	client, err := localhttp.New(name, ep, d.Keys, d.StagingDir)
	if err != nil {
		st.Reason = "接続先を使えません: " + err.Error()
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, comfyProbeTimeout)
	defer cancel()
	data, err := client.Request(ctx, http.MethodGet, "/system_stats", nil, nil, localhttp.MaxJSONBytes)
	if err != nil {
		st.Reason = "ComfyUI に接続できません: " + err.Error()
		return st
	}
	st.Reachable = true
	st.Devices, st.Version, err = parseSystemStats(data)
	if err != nil {
		st.Unavailable["devices"] = "/system_stats の答えを読めませんでした: " + err.Error()
	}
	data, err = client.Request(ctx, http.MethodGet, "/queue", nil, nil, localhttp.MaxJSONBytes)
	if err != nil {
		st.Unavailable["queue"] = "/queue を読めませんでした: " + err.Error()
		return st
	}
	if st.Running, st.Pending, err = parseQueue(name, data, live.Default); err != nil {
		st.Unavailable["queue"] = "/queue の答えを読めませんでした: " + err.Error()
	}
	return st
}
