package workflows

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// Workflow is a saved workflow.
type Workflow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind" jsonschema:"image または video"`
	Endpoint string `json:"endpoint" jsonschema:"実行する ComfyUI の接続先（既定: bundled-comfyui）"`
	Version  int    `json:"version" jsonschema:"現在の版。グラフか割り当てを変えると上がる"`
	ModelID  string `json:"model_id" jsonschema:"現在の版の識別子 comfy:<id>@v<N>。take.model_name と set_shot_workflow に使う"`
	Ready    bool   `json:"ready" jsonschema:"実行できる（用途・Policy・Shot で選べる）"`
	// Problems say why it cannot run (English, from the check a registered model gets).
	Problems  []string `json:"problems,omitempty"`
	HasEditor bool     `json:"has_editor_format" jsonschema:"画面の形式も保存している（ノード画面で開き直せる）"`
	UsedBy    []string `json:"used_by,omitempty" jsonschema:"このワークフローを選んでいる用途・Policy（images.new_reference.primary など）"`
	UpdatedAt string   `json:"updated_at"`
}

func workflowOf(w workflowsvc.Workflow) Workflow {
	return Workflow{
		ID: w.ID, Name: w.Name, Kind: string(w.Kind), Endpoint: w.Endpoint, Version: w.Version, ModelID: w.ModelID, Ready: w.Ready,
		Problems: w.Problems, HasEditor: w.HasUI, UsedBy: w.UsedBy, UpdatedAt: w.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// ListOutput lists the workflows.
type ListOutput struct {
	Workflows []Workflow `json:"workflows"`
}

// Empty has no field.
type Empty struct{}

func (h handlers) list(ctx context.Context, _ Empty) (ListOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return ListOutput{}, err
	}
	list, err := svc.List(ctx)
	if err != nil {
		return ListOutput{}, mapError(err)
	}
	out := ListOutput{Workflows: make([]Workflow, len(list))}
	for i, w := range list {
		out.Workflows[i] = workflowOf(w)
	}
	return out, nil
}

// GetInput names a workflow.
type GetInput struct {
	WorkflowID   string `json:"workflow_id"`
	IncludeGraph bool   `json:"include_graph,omitempty" jsonschema:"API 形式のグラフも返す（大きいことがある）"`
}

// Node is a node of the graph with the inputs a value can go to.
type Node struct {
	ID     string   `json:"id"`
	Class  string   `json:"class_type"`
	Inputs []string `json:"inputs" jsonschema:"値を入れられる入力（ほかのノードがつないでいないもの）"`
}

// VersionInfo is one version of a workflow.
type VersionInfo struct {
	Version int    `json:"version"`
	ModelID string `json:"model_id"`
	Current bool   `json:"current"`
}

// GetOutput is a workflow with its settings.
type GetOutput struct {
	Workflow
	Settings Settings       `json:"settings"`
	Nodes    []Node         `json:"nodes"`
	Versions []VersionInfo  `json:"versions"`
	Graph    map[string]any `json:"graph,omitempty" jsonschema:"API 形式のグラフ（include_graph のとき）"`
}

func (h handlers) get(ctx context.Context, in GetInput) (GetOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return GetOutput{}, err
	}
	d, err := svc.Get(ctx, in.WorkflowID)
	if err != nil {
		return GetOutput{}, mapError(err)
	}
	out := GetOutput{Workflow: workflowOf(d.Workflow), Settings: settingsOf(d.Spec), Nodes: []Node{}, Versions: []VersionInfo{}}
	for _, n := range d.Nodes {
		node := Node{ID: n.ID, Class: n.Class, Inputs: []string{}}
		for _, i := range n.Inputs {
			if !i.Link {
				node.Inputs = append(node.Inputs, i.Name)
			}
		}
		out.Nodes = append(out.Nodes, node)
	}
	versions, err := svc.Versions(ctx, in.WorkflowID)
	if err != nil {
		return GetOutput{}, mapError(err)
	}
	for _, v := range versions {
		out.Versions = append(out.Versions, VersionInfo{Version: v.Version, ModelID: v.ModelID, Current: v.Current})
	}
	if in.IncludeGraph {
		if err := json.Unmarshal(d.APIJSON, &out.Graph); err != nil {
			return GetOutput{}, err
		}
	}
	return out, nil
}

// RenameInput renames a workflow.
type RenameInput struct {
	WorkflowID string `json:"workflow_id"`
	Name       string `json:"name" jsonschema:"新しい名前（100 文字以内）"`
}

func (h handlers) rename(ctx context.Context, in RenameInput) (Workflow, error) {
	svc, err := h.workflows()
	if err != nil {
		return Workflow{}, err
	}
	w, err := svc.Update(ctx, in.WorkflowID, workflowsvc.UpdateInput{Name: &in.Name})
	return workflowOf(w), mapError(err)
}

// DeleteOutput confirms a deletion.
type DeleteOutput struct {
	Deleted bool `json:"deleted"`
}

func (h handlers) delete(ctx context.Context, in Input) (DeleteOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return DeleteOutput{}, err
	}
	if err := svc.Delete(ctx, in.WorkflowID); err != nil {
		return DeleteOutput{}, mapError(err)
	}
	return DeleteOutput{Deleted: true}, nil
}

// UpdateAssignmentInput replaces the settings of a workflow.
type UpdateAssignmentInput struct {
	WorkflowID string `json:"workflow_id"`
	Settings
}

func (h handlers) updateAssignment(ctx context.Context, in UpdateAssignmentInput) (SavedOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return SavedOutput{}, err
	}
	spec := in.spec()
	w, err := svc.Update(ctx, in.WorkflowID, workflowsvc.UpdateInput{Spec: &spec})
	return saved(ctx, svc, w, err)
}

// ImportJSONInput is an API-format workflow to save.
type ImportJSONInput struct {
	Name     string    `json:"name" jsonschema:"名前（100 文字以内。同じ名前は不可）"`
	Kind     string    `json:"kind" jsonschema:"image または video"`
	Endpoint string    `json:"endpoint,omitempty" jsonschema:"実行する ComfyUI の接続先（省略: bundled-comfyui）"`
	APIJSON  string    `json:"api_json" jsonschema:"「Save (API Format)」の JSON を文字列で（{\"prompt\": ...} も可）。エディタ形式は拒否される"`
	Settings *Settings `json:"settings,omitempty" jsonschema:"入力の割り当て。省略すると一般的なノードから推測する"`
}

func (h handlers) importJSON(ctx context.Context, in ImportJSONInput) (SavedOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return SavedOutput{}, err
	}
	create := workflowsvc.CreateInput{Name: in.Name, Kind: localcfg.Media(in.Kind), Endpoint: in.Endpoint, API: []byte(in.APIJSON)}
	if in.Settings != nil {
		spec := in.Settings.spec()
		create.Spec = &spec
	}
	w, err := svc.Create(ctx, create)
	return saved(ctx, svc, w, err)
}

// SavedOutput is a saved workflow with what the connected ComfyUI lacks for it, found at the save (never a reason
// to refuse the save: the server may get the node or the file later).
type SavedOutput struct {
	Workflow
	Check CheckOutput `json:"check" jsonschema:"保存時の ComfyUI との照合（足りないノード・モデルの警告）"`
}

func saved(ctx context.Context, svc *workflowsvc.Service, w workflowsvc.Workflow, err error) (SavedOutput, error) {
	if err != nil {
		return SavedOutput{}, mapError(err)
	}
	res, err := svc.Check(ctx, w.ID)
	if err != nil {
		return SavedOutput{}, mapError(err)
	}
	return SavedOutput{Workflow: workflowOf(w), Check: checkOf(res)}, nil
}
