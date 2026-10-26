// Package workflows holds the MCP tools of the ComfyUI workflows saved in mam: list, get, rename, delete, the
// input assignment, the import of an API-format JSON or of a workflow of the ComfyUI server, the check against the
// connected ComfyUI, the run with given inputs (job_id; results as token URLs) and the pin of a Shot to one
// (spec/api/mcp-tools-workflows.md, spec/architecture/comfy/comfy-workflows.md). The node screen itself (the ComfyUI GUI)
// is not a tool: tools work on the workflow's JSON. Without the service (svc.Workflows is nil) every tool answers
// "unavailable".
package workflows

import (
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// Register adds the tools of this package, the download kind of a run's result and the Probe of the run job.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	h := handlers{svc: svc, r: r}
	r.Files.RegisterDownload(KindRunResult, h.runFile)
	r.Jobs.RegisterKind(KindRunWorkflow, h.runProbe)

	mcpkit.Read(r, "list_workflows",
		"mam に保存した ComfyUI ワークフローの一覧（識別子 comfy:<id>@v<N>・種類・版・実行できるか・使われている用途）/ List the ComfyUI workflows saved in mam.",
		h.list)
	mcpkit.Read(r, "get_workflow",
		"ワークフローを 1 件取得する（入力の割り当て・出力ノード・版の一覧。include_graph で API 形式のグラフも）/ One workflow with its input assignment and versions.",
		h.get)
	mcpkit.Write(r, "rename_workflow",
		"ワークフローの名前を変える（版は上がらない。識別子は ID でできているので変わらない）/ Rename a workflow (no new version).",
		h.rename, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_workflow",
		"ワークフローを削除する（全版）。画像の用途・Quality Policy が使っていれば拒否される。過去の Take には識別子が残る / Delete a workflow; refused while a purpose or policy uses it.",
		h.delete)
	mcpkit.Write(r, "update_workflow_assignment",
		"ワークフローの入力の割り当て（mam の値をどのノードのどの入力に入れるか）・出力ノード・動画の能力を置き換える。実行できた版が変わると版が上がる / Replace the input assignment; a change of a version that could run makes a new version.",
		h.updateAssignment, mcpkit.Idempotent())
	mcpkit.Write(r, "import_workflow_json",
		"ComfyUI の「Save (API Format)」の JSON（文字列）を新しいワークフローとして保存する。assignments を省くと一般的なノードから推測する（結果の problems で足りないものが分かる）/ Save an API-format workflow JSON as a new workflow.",
		h.importJSON)
	mcpkit.Read(r, "list_comfyui_workflows",
		"ComfyUI サーバーが自分に保存しているワークフローの一覧（import_workflow_from_comfyui の path に使う）。同梱の ComfyUI は止まっていれば起動する / List the workflows the ComfyUI server holds.",
		h.listRemote, mcpkit.OpenWorld())
	mcpkit.Write(r, "import_workflow_from_comfyui",
		"ComfyUI のワークフローを mam に取り込む。画面の形式は保存済みのファイル（path）から、API 形式はその最後の実行（履歴）から読む。path を省くと最後の実行。replace_workflow_id で既存を更新 / Import a workflow of the ComfyUI server (its saved file and the API format of its last run).",
		h.importRemote, mcpkit.OpenWorld())
	mcpkit.Read(r, "check_workflow",
		"つないでいる ComfyUI に、ワークフローのノードとモデルファイルがあるか確かめる（GET /object_info と /models）。足りないものは warnings で返し、実行は止めない。同梱の ComfyUI は起動しない / Warn about nodes and model files the connected ComfyUI lacks.",
		h.check, mcpkit.OpenWorld())
	mcpkit.Write(r, "run_workflow",
		"保存したワークフローを入力（prompt・画像・解像度・長さ）つきで実行する。job_id を返し、get_job で進捗と結果（download_url）を取る。同梱の ComfyUI は止まっていれば起動する。Project には何も書かない / Run a saved workflow with inputs; returns a job_id.",
		h.run, mcpkit.OpenWorld())
	mcpkit.Write(r, "set_shot_workflow",
		"Shot の生成方法をワークフローに固定する（Video Model を comfy:<id>@v<N> で LOCK）。動画のワークフローだけ。解除は set_shot_lock の video_model を auto にする / Pin a Shot to a saved video workflow.",
		h.setShotWorkflow, mcpkit.Idempotent())
}

type handlers struct {
	svc *service.Services
	r   *mcpkit.Registrar
}

// workflows is the service, or the "unavailable" error.
func (h handlers) workflows() (*workflowsvc.Service, error) {
	if h.svc.Workflows == nil {
		return nil, &mcpkit.ToolError{Code: mcpkit.CodeUnavailable, Message: "ComfyUI ワークフローはこのアプリでは使えません", Status: http.StatusServiceUnavailable}
	}
	return h.svc.Workflows, nil
}

// mapError turns the refusals of the service into the errors the tools map.
func mapError(err error) error {
	var invalid *workflowsvc.InvalidError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, workflowsvc.ErrExists):
		return &service.ConflictError{Message: "同じ名前のワークフローがすでにあります"}
	case errors.Is(err, workflowsvc.ErrNotFound):
		return &mcpkit.ToolError{Code: mcpkit.CodeNotFound, Message: "そのワークフロー（または版）は見つかりません（list_workflows で確かめてください）", Status: http.StatusNotFound}
	case errors.As(err, &invalid):
		return &service.ValidationError{Message: invalid.Message}
	}
	return err
}

// Input names a workflow.
type Input struct {
	WorkflowID string `json:"workflow_id" jsonschema:"ワークフローの ID（list_workflows の workflows[].id）"`
}

// Assignment says which input of which node a value of mam goes to.
type Assignment struct {
	Param string `json:"param" jsonschema:"prompt, negative_prompt, seed, width, height, reference_image, frames, fps, first_frame or last_frame（画像は frames・fps・first_frame・last_frame を使えない）"`
	Node  string `json:"node" jsonschema:"ノード ID（get_workflow の nodes[].id）"`
	Input string `json:"input" jsonschema:"そのノードの入力名。ほかのノードがつないでいる入力は使えない"`
	Index int    `json:"index,omitempty" jsonschema:"reference_image だけ: 何枚目か（0 から、抜けなし）"`
}

// VideoCapabilities is what a video workflow can make.
type VideoCapabilities struct {
	Durations   []float64 `json:"durations" jsonschema:"秒。小さい順"`
	Resolutions []string  `json:"resolutions" jsonschema:"短辺。例: 480p, 720p"`
	NativeAudio bool      `json:"native_audio,omitempty" jsonschema:"音声も同時に生成する"`
}

// Settings is the input assignment, the output node and the frame settings of a workflow.
type Settings struct {
	Assignments  []Assignment       `json:"assignments"`
	OutputNode   string             `json:"output_node" jsonschema:"画像・動画を保存するノードの ID"`
	FPS          int                `json:"fps,omitempty" jsonschema:"動画: フレームレート（省略: 16）"`
	FramesStep   int                `json:"frames_step,omitempty" jsonschema:"動画: 総フレーム数を n×step+1 にする（Wan は 4、LTX は 8）"`
	SizeMultiple int                `json:"size_multiple,omitempty" jsonschema:"幅・高さを丸める倍数（省略: 16）"`
	Video        *VideoCapabilities `json:"video,omitempty" jsonschema:"動画: 長さと解像度。省略すると既定（3・4・5 秒、480p・720p）"`
}

func settingsOf(s localcfg.WorkflowSpec) Settings {
	out := Settings{Assignments: make([]Assignment, 0, len(s.Assignments)), OutputNode: s.OutputNode, FPS: s.FPS, FramesStep: s.FramesStep, SizeMultiple: s.SizeMultiple}
	for _, a := range s.Assignments {
		out.Assignments = append(out.Assignments, Assignment{Param: string(a.Param), Node: a.Node, Input: a.Input, Index: a.Index})
	}
	if v := s.Video; v != nil {
		out.Video = &VideoCapabilities{Durations: v.Durations, Resolutions: v.Resolutions, NativeAudio: v.NativeAudio}
	}
	return out
}

func (s Settings) spec() localcfg.WorkflowSpec {
	out := localcfg.WorkflowSpec{OutputNode: s.OutputNode, FPS: s.FPS, FramesStep: s.FramesStep, SizeMultiple: s.SizeMultiple}
	for _, a := range s.Assignments {
		out.Assignments = append(out.Assignments, localcfg.Assignment{Param: localcfg.Param(a.Param), Node: a.Node, Input: a.Input, Index: a.Index})
	}
	if v := s.Video; v != nil {
		out.Video = &localcfg.VideoSettings{Durations: v.Durations, Resolutions: v.Resolutions, NativeAudio: v.NativeAudio}
	}
	return out
}
