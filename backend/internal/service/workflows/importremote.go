package workflows

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// RemoteWorkflow is a workflow saved in a ComfyUI server's own user data (its workflows folder).
type RemoteWorkflow struct {
	// Path is the file under the workflows folder ("hero.json", "wan/i2v.json").
	Path string `json:"path"`
	// Name is the file name without ".json": what the import calls the workflow by default.
	Name     string `json:"name"`
	Size     int64  `json:"size,omitempty"`
	Modified int64  `json:"modified,omitempty"`
}

// ListRemote lists the workflows saved in the ComfyUI server (GET /userdata?dir=workflows), newest first. The
// bundled ComfyUI is started when it is stopped: the user asked for what it holds.
func (s *Service) ListRemote(ctx context.Context, endpoint string) ([]RemoteWorkflow, error) {
	endpoint, err := cleanEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	c, err := s.client(ctx, endpoint, true)
	if err != nil {
		return nil, err
	}
	var raw []json.RawMessage
	if err := getJSON(ctx, c, "/userdata?dir=workflows&recurse=true&full_info=true", &raw); err != nil {
		return nil, err
	}
	out := make([]RemoteWorkflow, 0, len(raw))
	for _, r := range raw {
		var full struct {
			Path     string  `json:"path"`
			Size     int64   `json:"size"`
			Modified float64 `json:"modified"`
		}
		if json.Unmarshal(r, &full) != nil || full.Path == "" {
			if json.Unmarshal(r, &full.Path) != nil { // the plain list: an array of paths
				continue
			}
		}
		full.Path = strings.ReplaceAll(full.Path, "\\", "/")
		if !strings.HasSuffix(full.Path, ".json") {
			continue
		}
		out = append(out, RemoteWorkflow{Path: full.Path, Name: strings.TrimSuffix(path.Base(full.Path), ".json"), Size: full.Size, Modified: int64(full.Modified)})
	}
	slices.SortStableFunc(out, func(a, b RemoteWorkflow) int { return int(b.Modified - a.Modified) })
	return out, nil
}

// ImportRemoteInput says which workflow of the ComfyUI server to bring into mam.
type ImportRemoteInput struct {
	// Endpoint is the ComfyUI endpoint ("": the bundled ComfyUI); the workflow runs there afterwards.
	Endpoint string
	// Path is the saved workflow to import (from ListRemote); "" takes the last run, whatever workflow it was.
	Path string
	// Name is the name in mam ("": the file's name). Kind is image or video ("": guessed from the Save node).
	Name string
	Kind localcfg.Media
	// Replace is the ID of a saved workflow to bring up to date (its graph and editor format) instead of
	// saving a new one.
	Replace string
}

// ImportFromComfyUI brings a workflow of the ComfyUI server into mam. The editor's format comes from the server's
// saved workflow; the API format, which the server does not save, comes from the last run of that workflow in its
// history (the prompt and the workflow the editor attached to it): run it once in the node screen first.
func (s *Service) ImportFromComfyUI(ctx context.Context, in ImportRemoteInput) (Workflow, error) {
	endpoint, err := cleanEndpoint(in.Endpoint)
	if err != nil {
		return Workflow{}, err
	}
	c, err := s.client(ctx, endpoint, true)
	if err != nil {
		return Workflow{}, err
	}
	var history map[string]json.RawMessage
	if err := getJSON(ctx, c, "/history?max_items=64", &history); err != nil {
		return Workflow{}, err
	}
	runs := parseRuns(history)
	ui, name := "", in.Name
	if in.Path != "" {
		file := strings.TrimPrefix(in.Path, "/")
		if strings.Contains(file, "..") || !strings.HasSuffix(file, ".json") {
			return Workflow{}, invalid("ワークフローのファイル名が正しくありません")
		}
		data, err := c.Request(ctx, "GET", "/userdata/"+url.PathEscape("workflows/"+file), nil, nil, maxUIBytes)
		if err != nil {
			return Workflow{}, invalid("ComfyUI からワークフロー %s を読めません: %s", file, reasonOf(err))
		}
		ui = string(data)
		if name == "" {
			name = strings.TrimSuffix(path.Base(file), ".json")
		}
	}
	run, ok := lastRunOf(runs, ui)
	if !ok {
		return Workflow{}, invalid("このワークフローの実行履歴が ComfyUI にありません。ノード画面で一度実行してから、もう一度取り込んでください")
	}
	if ui == "" {
		ui = string(run.ui)
	}
	if name == "" {
		name = "ComfyUI から取り込み"
	}
	if in.Replace != "" {
		return s.Update(ctx, in.Replace, UpdateInput{API: run.graph, UI: &ui})
	}
	kind := in.Kind
	if kind == "" {
		kind = guessKind(run.graph)
	}
	return s.Create(ctx, CreateInput{Name: name, Kind: kind, Endpoint: endpoint, API: run.graph, UI: ui})
}

// run is one entry of /history: the API-format prompt that ran and the editor's workflow attached to it.
type run struct {
	order float64
	graph json.RawMessage
	ui    json.RawMessage
}

// parseRuns reads GET /history. An entry's "prompt" is [queue number, prompt id, graph, extra_data, outputs]; the
// editor's workflow is extra_data.extra_pnginfo.workflow. An entry in another shape is skipped.
func parseRuns(history map[string]json.RawMessage) []run {
	var runs []run
	for _, raw := range history {
		var entry struct {
			Prompt []json.RawMessage `json:"prompt"`
		}
		if json.Unmarshal(raw, &entry) != nil || len(entry.Prompt) < 3 {
			continue
		}
		var r run
		_ = json.Unmarshal(entry.Prompt[0], &r.order)
		r.graph = entry.Prompt[2]
		if len(entry.Prompt) > 3 {
			var extra struct {
				ExtraPnginfo struct {
					Workflow json.RawMessage `json:"workflow"`
				} `json:"extra_pnginfo"`
			}
			if json.Unmarshal(entry.Prompt[3], &extra) == nil {
				r.ui = extra.ExtraPnginfo.Workflow
			}
		}
		if _, err := localcfg.ParseGraph(r.graph); err == nil {
			runs = append(runs, r)
		}
	}
	slices.SortFunc(runs, func(a, b run) int { return int(b.order - a.order) })
	return runs
}

// lastRunOf is the newest run of the workflow whose editor format is ui (matched by the workflow's id), or the
// newest run when ui is "".
func lastRunOf(runs []run, ui string) (run, bool) {
	if len(runs) == 0 {
		return run{}, false
	}
	if ui == "" {
		return runs[0], true
	}
	want := workflowKey(json.RawMessage(ui))
	for _, r := range runs {
		if want != "" && workflowKey(r.ui) == want {
			return r, true
		}
	}
	return run{}, false
}

// workflowKey is the id the editor gives a workflow (the "id" of its JSON), which its runs carry too.
func workflowKey(ui json.RawMessage) string {
	var w struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(ui, &w)
	return w.ID
}

// guessKind says video when the graph saves a video, image otherwise.
func guessKind(graph json.RawMessage) localcfg.Media {
	g, err := localcfg.ParseGraph(graph)
	if err != nil {
		return localcfg.MediaImage
	}
	for _, n := range g {
		if isSaveNode(n.ClassType, localcfg.MediaVideo) {
			return localcfg.MediaVideo
		}
	}
	return localcfg.MediaImage
}
