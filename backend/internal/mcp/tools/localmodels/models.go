package localmodels

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

func registerModels(r *mcpkit.Registrar, h handlers) {
	mcpkit.Read(r, "list_local_templates",
		"ComfyUI の組み込みワークフローのテンプレート（画像 txt2img / img2img、動画 txt2video / img2video。コアノードだけ）。create_local_model の template_id に使う / Built-in ComfyUI workflow templates.",
		h.listTemplates)
	mcpkit.Read(r, "import_local_workflow",
		"ComfyUI の「Save (API Format)」の JSON を読み、ノードとその入力の一覧を返す（保存はしない）。割り当て（assignments）と output_node を決める材料にする。エディタ形式は拒否する / Parse an API-format workflow and list its nodes; nothing is saved.",
		h.importWorkflow)
	mcpkit.Read(r, "list_local_models",
		"登録済みのローカル・代替モデルの一覧（識別子・能力・使われている用途 / Policy）/ Models defined on the endpoints.",
		h.listModels)
	mcpkit.Read(r, "get_local_model",
		"モデルを 1 件取得する（割り当て表と出力ノードを含む）/ One model with its workflow assignments.",
		h.getModel)
	mcpkit.Write(r, "create_local_model",
		"接続先にモデルを定義する。ComfyUI は template_id か workflow_json（と assignments・output_node）、OpenAI 互換は model_id。動画は video（長さ・解像度）が必要。識別子は <kind>:<endpoint>/<name>。保存するとすぐに効き、set_model_assignment の候補に加わる / Define a model on an endpoint (effective at once).",
		h.createModel)
	mcpkit.Write(r, "update_local_model",
		"モデルの定義を変える（名前・接続先は変えられない）。渡した項目だけが変わり、workflow_json・assignments を省くと保存済みのワークフローのまま / Change a model; omitted fields keep what is saved.",
		h.updateModel, mcpkit.Idempotent())
	mcpkit.Destroy(r, "delete_local_model",
		"モデルを削除する。画像の用途・Quality Policy に割り当てられていれば拒否される（先に set_model_assignment で変える）/ Delete a model; refused while it is assigned.",
		h.deleteModel)
}

// Assignment says which input of which node a value of mam goes to.
type Assignment struct {
	Param string `json:"param" jsonschema:"prompt, negative_prompt, seed, width, height, reference_image, frames, fps, first_frame or last_frame（画像は frames・fps・first_frame・last_frame を使えない）"`
	Node  string `json:"node" jsonschema:"ノード ID（import_local_workflow の nodes[].id）"`
	Input string `json:"input" jsonschema:"そのノードの入力名。ほかのノードがつないでいる入力は使えない"`
	Index int    `json:"index,omitempty" jsonschema:"reference_image だけ: 何枚目か（0 から、抜けなし）"`
}

// VideoCapabilities is what a video model can make.
type VideoCapabilities struct {
	Durations   []float64 `json:"durations" jsonschema:"秒。小さい順"`
	Resolutions []string  `json:"resolutions" jsonschema:"短辺。例: 480p, 720p"`
	NativeAudio bool      `json:"native_audio" jsonschema:"音声も同時に生成する"`
}

// Template is a built-in workflow.
type Template struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Media       string             `json:"media"`
	Description string             `json:"description"`
	Assignments []Assignment       `json:"assignments"`
	OutputNode  string             `json:"output_node"`
	Video       *VideoCapabilities `json:"video,omitempty"`
}

// ListTemplatesOutput is the built-in templates.
type ListTemplatesOutput struct {
	Templates []Template `json:"templates"`
}

func assignmentsOf(w *localcfg.Workflow) []Assignment {
	out := []Assignment{}
	if w == nil {
		return out
	}
	for _, a := range w.Assignments {
		out = append(out, Assignment{Param: string(a.Param), Node: a.Node, Input: a.Input, Index: a.Index})
	}
	return out
}

func videoOf(v *localcfg.VideoSettings) *VideoCapabilities {
	if v == nil {
		return nil
	}
	return &VideoCapabilities{Durations: slices.Clone(v.Durations), Resolutions: slices.Clone(v.Resolutions), NativeAudio: v.NativeAudio}
}

func (h handlers) listTemplates(_ context.Context, _ Input) (ListTemplatesOutput, error) {
	svc, err := h.local()
	if err != nil {
		return ListTemplatesOutput{}, err
	}
	out := ListTemplatesOutput{Templates: []Template{}}
	for _, t := range svc.Templates() {
		item := Template{ID: t.ID, Label: t.Label, Media: string(t.Media), Description: t.Description, Assignments: assignmentsOf(t.Model.Workflow), Video: videoOf(t.Model.Video)}
		if t.Model.Workflow != nil {
			item.OutputNode = t.Model.Workflow.OutputNode
		}
		out.Templates = append(out.Templates, item)
	}
	return out, nil
}

// ImportInput is a workflow to read.
type ImportInput struct {
	WorkflowJSON string `json:"workflow_json" jsonschema:"ComfyUI の Save (API Format) の JSON（{\"prompt\": ...} の形も可）"`
}

// InputInfo is an input of a node.
type InputInfo struct {
	Name   string `json:"name"`
	Linked bool   `json:"linked" jsonschema:"ほかのノードの出力がつながっている（値は入れられない）"`
	Value  string `json:"value,omitempty" jsonschema:"つながっていない入力の今の値（長いものは省略）"`
}

// NodeInfo is a node of the workflow.
type NodeInfo struct {
	ID        string      `json:"id"`
	ClassType string      `json:"class_type"`
	Inputs    []InputInfo `json:"inputs"`
}

// ImportOutput is the nodes of the workflow and the values of mam a workflow can take.
type ImportOutput struct {
	Nodes       []NodeInfo `json:"nodes"`
	ImageParams []string   `json:"image_params" jsonschema:"画像のワークフローで割り当てられる param"`
	VideoParams []string   `json:"video_params" jsonschema:"動画のワークフローで割り当てられる param"`
}

func paramNames(ps []localcfg.Param) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}

func (h handlers) importWorkflow(_ context.Context, in ImportInput) (ImportOutput, error) {
	svc, err := h.local()
	if err != nil {
		return ImportOutput{}, err
	}
	imported, err := svc.ImportWorkflow([]byte(in.WorkflowJSON))
	if err != nil {
		return ImportOutput{}, mapError(err)
	}
	out := ImportOutput{Nodes: make([]NodeInfo, 0, len(imported.Nodes)), ImageParams: paramNames(localcfg.ImageParams()), VideoParams: paramNames(localcfg.VideoParams())}
	for _, n := range imported.Nodes {
		node := NodeInfo{ID: n.ID, ClassType: n.Class, Inputs: make([]InputInfo, 0, len(n.Inputs))}
		for _, i := range n.Inputs {
			node.Inputs = append(node.Inputs, InputInfo{Name: i.Name, Linked: i.Link, Value: i.Value})
		}
		out.Nodes = append(out.Nodes, node)
	}
	return out, nil
}

// Model is a model defined on an endpoint.
type Model struct {
	Endpoint          string             `json:"endpoint"`
	Name              string             `json:"name"`
	ID                string             `json:"id" jsonschema:"take.model_name に残る識別子。set_model_assignment で選ぶ値"`
	Kind              string             `json:"kind"`
	Media             string             `json:"media" jsonschema:"image or video"`
	ModelID           string             `json:"model_id,omitempty" jsonschema:"OpenAI 互換: サーバーに渡すモデル ID"`
	AcceptsReferences bool               `json:"accepts_references" jsonschema:"参照画像を受け取れる"`
	Assignments       []Assignment       `json:"assignments" jsonschema:"ComfyUI: 入力の割り当て"`
	OutputNode        string             `json:"output_node,omitempty"`
	FPS               int                `json:"fps,omitempty"`
	FramesStep        int                `json:"frames_step,omitempty"`
	SizeMultiple      int                `json:"size_multiple,omitempty"`
	Video             *VideoCapabilities `json:"video,omitempty"`
	MaxReferences     int                `json:"max_reference_images,omitempty" jsonschema:"動画: 参照画像の上限（割り当てから決まる）"`
	FirstFrame        bool               `json:"first_frame,omitempty"`
	LastFrame         bool               `json:"last_frame,omitempty"`
	CostPerImageUSD   float64            `json:"cost_per_image_usd"`
	CostPerSecondUSD  float64            `json:"cost_per_second_usd"`
	UsedBy            []string           `json:"used_by" jsonschema:"割り当て先（images.<purpose>.primary、policies.<policy>.fallback など）。あるうちは削除できない"`
}

func modelOf(m localmodels.ModelView) Model {
	out := Model{
		Endpoint: m.Endpoint, Name: m.Name, ID: m.ID, Kind: string(m.Kind), Media: string(m.Media), ModelID: m.Model.Model,
		AcceptsReferences: m.ImageInputs(m.Kind), Assignments: assignmentsOf(m.Workflow), Video: videoOf(m.Video),
		CostPerImageUSD: m.CostPerImage, CostPerSecondUSD: m.CostPerSecond, UsedBy: append([]string{}, m.UsedBy...),
	}
	if w := m.Workflow; w != nil {
		out.OutputNode, out.FPS, out.FramesStep, out.SizeMultiple = w.OutputNode, w.FPS, w.FramesStep, w.SizeMultiple
	}
	if caps, ok := m.VideoCaps(); ok {
		out.MaxReferences, out.FirstFrame, out.LastFrame = caps.MaxReferenceImages, caps.FirstFrame, caps.LastFrame
	}
	return out
}

// ListModelsOutput is every model.
type ListModelsOutput struct {
	Pinned bool    `json:"pinned"`
	Models []Model `json:"models"`
}

func (h handlers) listModels(_ context.Context, _ Input) (ListModelsOutput, error) {
	svc, err := h.local()
	if err != nil {
		return ListModelsOutput{}, err
	}
	v, err := svc.View()
	if err != nil {
		return ListModelsOutput{}, mapError(err)
	}
	out := ListModelsOutput{Pinned: v.Pinned, Models: make([]Model, 0, len(v.Models))}
	for _, m := range v.Models {
		out.Models = append(out.Models, modelOf(m))
	}
	return out, nil
}

// ModelRef names a model.
type ModelRef struct {
	Endpoint string `json:"endpoint" jsonschema:"接続先の名前"`
	Name     string `json:"name" jsonschema:"モデルの名前"`
}

func (h handlers) findModel(ref ModelRef) (localmodels.ModelView, error) {
	svc, err := h.local()
	if err != nil {
		return localmodels.ModelView{}, err
	}
	v, err := svc.View()
	if err != nil {
		return localmodels.ModelView{}, mapError(err)
	}
	i := slices.IndexFunc(v.Models, func(m localmodels.ModelView) bool { return m.Endpoint == ref.Endpoint && m.Name == ref.Name })
	if i < 0 {
		return localmodels.ModelView{}, mapError(localmodels.ErrNotFound)
	}
	return v.Models[i], nil
}

func (h handlers) getModel(_ context.Context, in ModelRef) (Model, error) {
	m, err := h.findModel(in)
	return modelOf(m), err
}
