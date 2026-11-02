package gpustate

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
)

func parseSystemStats(data []byte) ([]ComfyUIDevice, string, error) {
	var body struct {
		System struct {
			Version string `json:"comfyui_version"`
		} `json:"system"`
		Devices []struct {
			Name           string `json:"name"`
			Type           string `json:"type"`
			VRAMTotal      int64  `json:"vram_total"`
			VRAMFree       int64  `json:"vram_free"`
			TorchVRAMTotal int64  `json:"torch_vram_total"`
			TorchVRAMFree  int64  `json:"torch_vram_free"`
		} `json:"devices"`
	}
	devices := []ComfyUIDevice{}
	if err := json.Unmarshal(data, &body); err != nil {
		return devices, "", err
	}
	for _, d := range body.Devices {
		cd := ComfyUIDevice{Name: d.Name, Type: d.Type, VRAMTotal: d.VRAMTotal, VRAMFree: d.VRAMFree, TorchVRAMTotal: d.TorchVRAMTotal, TorchVRAMFree: d.TorchVRAMFree}
		if d.TorchVRAMTotal > 0 {
			cd.ComfyUIUsed = new(max(0, d.TorchVRAMTotal-d.TorchVRAMFree))
		} else {
			cd.UsedReason = "この ComfyUI は PyTorch のメモリの量（torch_vram_total）を返しません（CPU 実行か古い版）"
		}
		devices = append(devices, cd)
	}
	return devices, body.System.Version, nil
}

// parseQueue reads GET /queue: the entries are [number, prompt_id, prompt, extra_data, outputs]. The waiting ones are put in the order
// the server runs them (by number). Progress of a running job comes from reg, which holds the jobs mam itself queued.
func parseQueue(endpoint string, data []byte, reg *live.Registry) (running, pending []QueueJob, err error) {
	var q struct {
		Running [][]json.RawMessage `json:"queue_running"`
		Pending [][]json.RawMessage `json:"queue_pending"`
	}
	running, pending = []QueueJob{}, []QueueJob{}
	if err := json.Unmarshal(data, &q); err != nil {
		return running, pending, err
	}
	for _, e := range q.Running {
		if id, _, ok := queueEntry(e); ok {
			running = append(running, runningJob(endpoint, id, reg))
		}
	}
	type waiting struct {
		number float64
		id     string
	}
	var waits []waiting
	for _, e := range q.Pending {
		if id, number, ok := queueEntry(e); ok {
			waits = append(waits, waiting{number, id})
		}
	}
	slices.SortStableFunc(waits, func(a, b waiting) int {
		switch {
		case a.number < b.number:
			return -1
		case a.number > b.number:
			return 1
		}
		return 0
	})
	for i, w := range waits {
		pending = append(pending, QueueJob{PromptID: w.id, Position: i + 1, ProgressUnavailable: "順番待ちで、まだ始まっていません"})
	}
	return running, pending, nil
}

func queueEntry(e []json.RawMessage) (id string, number float64, ok bool) {
	if len(e) < 2 || json.Unmarshal(e[0], &number) != nil || json.Unmarshal(e[1], &id) != nil || id == "" {
		return "", 0, false
	}
	return id, number, true
}

func runningJob(endpoint, id string, reg *live.Registry) QueueJob {
	j := QueueJob{PromptID: id}
	p, ok := reg.Lookup(endpoint, id)
	switch {
	case !ok:
		j.ProgressUnavailable = "mam が投入したジョブではない（または mam が再起動した）ため、進み具合を受け取れません。ComfyUI は進み具合を、投入したクライアントにしか送りません"
	case p.MaxStep == 0:
		j.ProgressUnavailable = "ComfyUI がまだステップを報告していません（モデルの読み込みなどの準備中）"
	default:
		pr := &JobProgress{Node: p.Node, Step: p.Step, MaxStep: p.MaxStep, Percent: p.Percent}
		if p.HasETA {
			pr.ETASeconds = new(int(p.ETA.Round(time.Second) / time.Second))
		} else {
			pr.ETAUnavailable = "このノードのステップがまだ進んでいないため、残り時間を見積もれません"
		}
		j.Progress = pr
	}
	return j
}
