package workflows

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	workflowsvc "github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// EndpointInput names the ComfyUI endpoint.
type EndpointInput struct {
	Endpoint string `json:"endpoint,omitempty" jsonschema:"ComfyUI の接続先（省略: bundled-comfyui）"`
}

// RemoteWorkflow is a workflow the ComfyUI server holds.
type RemoteWorkflow struct {
	Path string `json:"path" jsonschema:"import_workflow_from_comfyui の path"`
	Name string `json:"name"`
}

// ListRemoteOutput lists them, newest first.
type ListRemoteOutput struct {
	Workflows []RemoteWorkflow `json:"workflows"`
}

func (h handlers) listRemote(ctx context.Context, in EndpointInput) (ListRemoteOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return ListRemoteOutput{}, err
	}
	list, err := svc.ListRemote(ctx, in.Endpoint)
	if err != nil {
		return ListRemoteOutput{}, mapError(err)
	}
	out := ListRemoteOutput{Workflows: make([]RemoteWorkflow, len(list))}
	for i, w := range list {
		out.Workflows[i] = RemoteWorkflow{Path: w.Path, Name: w.Name}
	}
	return out, nil
}

// ImportRemoteInput says which workflow of the ComfyUI server to import.
type ImportRemoteInput struct {
	Endpoint          string `json:"endpoint,omitempty" jsonschema:"ComfyUI の接続先（省略: bundled-comfyui）"`
	Path              string `json:"path,omitempty" jsonschema:"list_comfyui_workflows の path。省略すると最後の実行"`
	Name              string `json:"name,omitempty" jsonschema:"mam での名前（省略: ファイル名）"`
	Kind              string `json:"kind,omitempty" jsonschema:"image または video（省略: 保存ノードから判断）"`
	ReplaceWorkflowID string `json:"replace_workflow_id,omitempty" jsonschema:"既存のワークフローを更新する（グラフと画面の形式を入れ替え、版が上がる）"`
}

func (h handlers) importRemote(ctx context.Context, in ImportRemoteInput) (SavedOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return SavedOutput{}, err
	}
	w, err := svc.ImportFromComfyUI(ctx, workflowsvc.ImportRemoteInput{
		Endpoint: in.Endpoint, Path: in.Path, Name: in.Name, Kind: localcfg.Media(in.Kind), Replace: in.ReplaceWorkflowID,
	})
	return saved(ctx, svc, w, err)
}

// Warning is something the connected ComfyUI lacks.
type Warning struct {
	Kind    string `json:"kind" jsonschema:"node（ノードがない）, model（モデルファイルがない）, choice（選択肢にない値）"`
	Node    string `json:"node,omitempty"`
	Class   string `json:"class_type,omitempty"`
	Input   string `json:"input,omitempty"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message"`
}

// CheckOutput is the result of the check.
type CheckOutput struct {
	Endpoint  string    `json:"endpoint"`
	Reachable bool      `json:"reachable" jsonschema:"ComfyUI に問い合わせられた"`
	Reason    string    `json:"reason,omitempty" jsonschema:"問い合わせられなかった理由"`
	Warnings  []Warning `json:"warnings"`
}

func (h handlers) check(ctx context.Context, in Input) (CheckOutput, error) {
	svc, err := h.workflows()
	if err != nil {
		return CheckOutput{}, err
	}
	res, err := svc.Check(ctx, in.WorkflowID)
	if err != nil {
		return CheckOutput{}, mapError(err)
	}
	return checkOf(res), nil
}

func checkOf(res workflowsvc.CheckResult) CheckOutput {
	out := CheckOutput{Endpoint: res.Endpoint, Reachable: res.Reachable, Reason: res.Reason, Warnings: warningsOf(res.Warnings)}
	return out
}

func warningsOf(ws []workflowsvc.Warning) []Warning {
	out := make([]Warning, len(ws))
	for i, w := range ws {
		out[i] = Warning{Kind: w.Kind, Node: w.Node, Class: w.Class, Input: w.Input, Value: w.Value, Message: w.Message}
	}
	return out
}
