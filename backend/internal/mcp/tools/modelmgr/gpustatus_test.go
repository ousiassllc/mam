package modelmgr_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

const (
	statsBody = `{"system":{"comfyui_version":"0.42.1"},"devices":[{"name":"cuda:0 NVIDIA GeForce RTX 4070 Ti SUPER","type":"cuda","vram_total":17170000000,"vram_free":3000000000,"torch_vram_total":11000000000,"torch_vram_free":1000000000}]}`
	// the entries are [number, prompt_id, prompt, extra_data, outputs]; the waiting ones are not in order
	queueBody = `{"queue_running":[[3,"run-mam",{},{},[]]],"queue_pending":[[6,"wait-b",{},{},[]],[4,"wait-a",{},{},[]]]}`
)

func TestGetGPUStatus_IsListedReadOnlyAndAnswersNVIDIAAppleSiliconAndNothing(t *testing.T) {
	env, rig := newEnv(t)
	listed, err := env.Client.ListTools(t.Context(), nil)
	require.NoError(t, err)
	var found bool
	for _, tool := range listed.Tools {
		if tool.Name == "get_gpu_status" {
			found = true
			assert.True(t, tool.Annotations.ReadOnlyHint, "the tool reads and never changes anything")
		}
	}
	require.True(t, found, "get_gpu_status is in the tool list")

	gib := func(n int64) *int64 { n <<= 30; return &n }
	pct, deg := 87, 71
	t.Run("NVIDIA", func(t *testing.T) {
		rig.GPULive = gpu.Status{OS: "windows", Source: "nvidia-smi", Processes: []gpu.Process{},
			GPUs: []gpu.GPU{{Index: 0, Name: "NVIDIA GeForce RTX 4070 Ti SUPER", Vendor: "NVIDIA", Memory: "dedicated",
				VRAMTotalBytes: gib(16), VRAMUsedBytes: gib(9), VRAMFreeBytes: gib(7), UtilizationPercent: &pct, TemperatureCelsius: &deg}}}
		var out modelmgr.GPUStatusOutput
		env.CallOK("get_gpu_status", map[string]any{}, &out)
		require.Len(t, out.GPUs, 1)
		g := out.GPUs[0]
		assert.Equal(t, "NVIDIA GeForce RTX 4070 Ti SUPER", g.Name)
		assert.Equal(t, int64(7)<<30, *g.VRAMFreeBytes)
		assert.Equal(t, 87, *g.UtilizationPercent)
		assert.Equal(t, 71, *g.TemperatureCelsius)
		assert.NotNil(t, out.ComfyUI, "the list of ComfyUI endpoints is [] when there is none")
	})
	t.Run("Apple Silicon: unified memory, no temperature, with the reason", func(t *testing.T) {
		rig.GPULive = gpu.Status{OS: "darwin", Source: "ioreg", Processes: []gpu.Process{},
			GPUs: []gpu.GPU{{Name: "Apple M2 Pro", Vendor: "Apple", Memory: "unified", VRAMTotalBytes: gib(16), UtilizationPercent: &pct,
				Unavailable: map[string]string{"temperature_celsius": "powermetrics は管理者権限が要ります"}}}}
		var out modelmgr.GPUStatusOutput
		env.CallOK("get_gpu_status", map[string]any{}, &out)
		require.Len(t, out.GPUs, 1)
		assert.Equal(t, "unified", out.GPUs[0].Memory)
		assert.Nil(t, out.GPUs[0].TemperatureCelsius)
		assert.Contains(t, out.GPUs[0].Unavailable["temperature_celsius"], "powermetrics")
	})
	t.Run("nothing can be read: an answer with the reason, not an error", func(t *testing.T) {
		rig.GPULive = gpu.Status{OS: "linux", Source: "none", GPUs: []gpu.GPU{}, Processes: []gpu.Process{},
			Unavailable: map[string]string{"gpus": "nvidia-smi が見つかりません"}}
		var out modelmgr.GPUStatusOutput
		env.CallOK("get_gpu_status", map[string]any{}, &out)
		assert.Empty(t, out.GPUs)
		assert.Contains(t, out.Unavailable["gpus"], "nvidia-smi")
	})
	t.Run("unavailable without a model manager", func(t *testing.T) {
		env := mcptest.New(t, func(env *mcptest.Env, r *mcpkit.Registrar) { modelmgr.Register(r, env.Services) })
		assert.Contains(t, env.CallError("get_gpu_status", map[string]any{}), "unavailable")
	})
}

// get_gpu_status asks every ComfyUI endpoint (its VRAM, the running job with its progress, the waiting jobs in the order the
// server runs them) and only reads.
func TestGetGPUStatus_ReadsTheComfyUIQueue(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/system_stats":
			_, _ = w.Write([]byte(statsBody))
		case "/queue":
			_, _ = w.Write([]byte(queueBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	env, rig := newEnv(t)
	require.NoError(t, rig.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "tailnet", Kind: localcfg.KindComfyUI, BaseURL: srv.URL}))
	require.NoError(t, rig.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "compat", Kind: localcfg.KindOpenAICompat, BaseURL: srv.URL}))

	// mam queued "run-mam": its steps are known (the estimate of the time left is tested in package live, with a clock)
	job := live.Default.Start("tailnet", "run-mam")
	defer job.End()
	job.Step("3", "KSamplerAdvanced", 2, 20, 12)
	job.Step("3", "KSamplerAdvanced", 12, 20, 52)

	var out modelmgr.GPUStatusOutput
	env.CallOK("get_gpu_status", map[string]any{}, &out)
	require.Len(t, out.ComfyUI, 1, "only a ComfyUI endpoint is asked")
	c := out.ComfyUI[0]
	assert.Equal(t, "tailnet", c.Endpoint)
	assert.True(t, c.Reachable)
	assert.False(t, c.Bundled)
	assert.Equal(t, "0.42.1", c.Version)
	require.Len(t, c.Devices, 1)
	assert.EqualValues(t, 3_000_000_000, c.Devices[0].VRAMFree)
	require.NotNil(t, c.Devices[0].ComfyUIUsed)
	assert.EqualValues(t, 10_000_000_000, *c.Devices[0].ComfyUIUsed)
	assert.Empty(t, c.Unavailable)

	require.Len(t, c.Running, 1)
	assert.Equal(t, "run-mam", c.Running[0].PromptID)
	require.NotNil(t, c.Running[0].Progress)
	p := c.Running[0].Progress
	assert.Equal(t, 12, p.Step)
	assert.Equal(t, 20, p.MaxStep)
	assert.Equal(t, 52, p.Percent)
	assert.Equal(t, "KSamplerAdvanced", p.Node)

	require.Len(t, c.Pending, 2)
	assert.Equal(t, "wait-a", c.Pending[0].PromptID)
	assert.Equal(t, 1, c.Pending[0].Position)
	assert.Equal(t, "wait-b", c.Pending[1].PromptID)
	assert.Equal(t, 2, c.Pending[1].Position)
	assert.NotEmpty(t, c.Pending[0].ProgressUnavailable)

	mu.Lock()
	defer mu.Unlock()
	assert.NotEmpty(t, methods)
	for _, m := range methods {
		assert.Contains(t, m, "GET ", "the status only reads")
	}
}

// A job another client queued is running, but its steps are not known: the reason is given instead of a null. A server that is
// down is reported, not an error.
func TestGetGPUStatus_GivesReasonsForWhatItCannotRead(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/system_stats":
			_, _ = w.Write([]byte(`{"system":{"comfyui_version":"0.42.1"},"devices":[{"name":"cpu","type":"cpu","vram_total":0,"vram_free":0}]}`))
		case "/queue":
			_, _ = w.Write([]byte(`{"queue_running":[[1,"other-client",{},{},[]]],"queue_pending":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer up.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	env, rig := newEnv(t)
	require.NoError(t, rig.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "a-up", Kind: localcfg.KindComfyUI, BaseURL: up.URL}))
	require.NoError(t, rig.Models.CreateEndpoint(t.Context(), localmodels.EndpointInput{Name: "b-down", Kind: localcfg.KindComfyUI, BaseURL: downURL}))

	var out modelmgr.GPUStatusOutput
	env.CallOK("get_gpu_status", map[string]any{}, &out)
	require.Len(t, out.ComfyUI, 2)
	upState, downState := out.ComfyUI[0], out.ComfyUI[1]

	require.Len(t, upState.Devices, 1)
	assert.Nil(t, upState.Devices[0].ComfyUIUsed)
	assert.Contains(t, upState.Devices[0].UsedReason, "torch_vram_total")
	require.Len(t, upState.Running, 1)
	assert.Nil(t, upState.Running[0].Progress)
	assert.Contains(t, upState.Running[0].ProgressUnavailable, "投入したクライアントにしか送りません")

	assert.False(t, downState.Reachable)
	assert.Contains(t, downState.Reason, "接続できません")
	assert.NotNil(t, downState.Devices)
	assert.NotNil(t, downState.Running)
}
