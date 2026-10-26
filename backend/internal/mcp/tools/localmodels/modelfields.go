package localmodels

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
)

// ModelFields are the definition of a model. On create, what is not given is the template's (template_id)
// or empty; on update, what is not given is what is saved.
type ModelFields struct {
	TemplateID        string       `json:"template_id,omitempty" jsonschema:"ComfyUI: list_local_templates の id。ワークフロー・割り当て・動画の能力をテンプレートから始める（作成のみ）"`
	Media             string       `json:"media,omitempty" jsonschema:"image or video。テンプレートなしで作るときに必要（OpenAI 互換は image のみ）"`
	WorkflowJSON      string       `json:"workflow_json,omitempty" jsonschema:"ComfyUI: Save (API Format) の JSON。渡すと assignments も渡す必要がある"`
	Assignments       []Assignment `json:"assignments,omitempty" jsonschema:"ComfyUI: 入力の割り当て（渡すと全体を置き換える）。prompt が必須"`
	OutputNode        string       `json:"output_node,omitempty" jsonschema:"ComfyUI: 出力に使うノード ID"`
	FPS               int          `json:"fps,omitempty" jsonschema:"ComfyUI 動画: 既定 16"`
	FramesStep        int          `json:"frames_step,omitempty" jsonschema:"ComfyUI 動画: 正なら総フレーム数を n×step+1 にする（Wan は 4、LTX は 8）"`
	SizeMultiple      int          `json:"size_multiple,omitempty" jsonschema:"ComfyUI: 幅・高さの倍数。既定 16"`
	ModelID           string       `json:"model_id,omitempty" jsonschema:"OpenAI 互換: サーバーに渡すモデル ID"`
	AcceptsReferences *bool        `json:"accepts_references,omitempty" jsonschema:"OpenAI 互換: 参照画像を受け取れる（/v1/images/edits を使う）"`
	Durations         []float64    `json:"durations,omitempty" jsonschema:"動画: 長さ（秒、小さい順）"`
	Resolutions       []string     `json:"resolutions,omitempty" jsonschema:"動画: 解像度（短辺。例 480p, 720p）"`
	NativeAudio       *bool        `json:"native_audio,omitempty" jsonschema:"動画: 音声も同時に生成する"`
	CostPerImageUSD   *float64     `json:"cost_per_image_usd,omitempty" jsonschema:"見積コスト（USD / 枚）。既定 0"`
	CostPerSecondUSD  *float64     `json:"cost_per_second_usd,omitempty" jsonschema:"見積コスト（USD / 秒）。既定 0"`
}

// CreateModelInput is a model to define.
type CreateModelInput struct {
	Endpoint string `json:"endpoint" jsonschema:"接続先の名前"`
	Name     string `json:"name" jsonschema:"英小文字・数字・. _ - の 1〜63 文字。識別子の一部になり、後から変えられない"`
	ModelFields
}

// UpdateModelInput changes a model; Endpoint and Name say which, they cannot change.
type UpdateModelInput struct {
	ModelRef
	ModelFields
}

// apply puts the fields given over base.
func (f ModelFields) apply(base localcfg.Model) (localcfg.Model, error) {
	m := base
	if f.Media != "" {
		m.Media = localcfg.Media(f.Media)
	}
	if f.ModelID != "" {
		m.Model = f.ModelID
	}
	if f.AcceptsReferences != nil {
		m.AcceptsImageInputs = *f.AcceptsReferences
	}
	if f.CostPerImageUSD != nil {
		m.CostPerImage = *f.CostPerImageUSD
	}
	if f.CostPerSecondUSD != nil {
		m.CostPerSecond = *f.CostPerSecondUSD
	}
	if len(f.Durations) > 0 || len(f.Resolutions) > 0 || f.NativeAudio != nil {
		v := localcfg.VideoSettings{}
		if base.Video != nil {
			v = *base.Video
		}
		if len(f.Durations) > 0 {
			v.Durations = f.Durations
		}
		if len(f.Resolutions) > 0 {
			v.Resolutions = f.Resolutions
		}
		if f.NativeAudio != nil {
			v.NativeAudio = *f.NativeAudio
		}
		m.Video = &v
	}
	if f.touchesWorkflow() {
		w := localcfg.Workflow{}
		if base.Workflow != nil {
			w = *base.Workflow
		}
		if f.WorkflowJSON != "" {
			w.Graph = json.RawMessage(f.WorkflowJSON)
		}
		if f.Assignments != nil {
			w.Assignments = make([]localcfg.Assignment, len(f.Assignments))
			for i, a := range f.Assignments {
				w.Assignments[i] = localcfg.Assignment{Param: localcfg.Param(a.Param), Node: a.Node, Input: a.Input, Index: a.Index}
			}
		}
		if f.OutputNode != "" {
			w.OutputNode = f.OutputNode
		}
		w.FPS, w.FramesStep, w.SizeMultiple = nonZero(f.FPS, w.FPS), nonZero(f.FramesStep, w.FramesStep), nonZero(f.SizeMultiple, w.SizeMultiple)
		m.Workflow = &w
	}
	if m.Media != localcfg.MediaVideo {
		m.Video = nil
	}
	return m, nil
}

func nonZero(v, fallback int) int {
	if v != 0 {
		return v
	}
	return fallback
}

func (f ModelFields) touchesWorkflow() bool {
	return f.WorkflowJSON != "" || f.Assignments != nil || f.OutputNode != "" || f.FPS != 0 || f.FramesStep != 0 || f.SizeMultiple != 0
}

func (h handlers) createModel(ctx context.Context, in CreateModelInput) (Model, error) {
	svc, err := h.local()
	if err != nil {
		return Model{}, err
	}
	var base localcfg.Model
	if in.TemplateID != "" {
		t, ok := localcfg.TemplateByID(in.TemplateID)
		if !ok {
			return Model{}, &service.ValidationError{Field: "template_id", Message: "そのテンプレートはありません（list_local_templates で確かめてください）"}
		}
		base = t.Model
	}
	if in.WorkflowJSON != "" { // a workflow of one's own: read as the screen reads it, so that a bad one says why
		imported, err := svc.ImportWorkflow([]byte(in.WorkflowJSON))
		if err != nil {
			return Model{}, mapError(err)
		}
		in.WorkflowJSON = string(imported.Graph)
	}
	m, err := in.apply(base)
	if err != nil {
		return Model{}, err
	}
	if err := svc.CreateModel(ctx, localmodels.ModelInput{Endpoint: in.Endpoint, Name: in.Name, Model: m}); err != nil {
		return Model{}, mapError(err)
	}
	return h.getModel(ctx, ModelRef{Endpoint: in.Endpoint, Name: in.Name})
}

func (h handlers) updateModel(ctx context.Context, in UpdateModelInput) (Model, error) {
	svc, err := h.local()
	if err != nil {
		return Model{}, err
	}
	cur, err := h.findModel(in.ModelRef)
	if err != nil {
		return Model{}, err
	}
	if in.TemplateID != "" {
		return Model{}, &service.ValidationError{Field: "template_id", Message: "template_id は作成のときだけ使えます。ワークフローを替えるには workflow_json と assignments を渡してください"}
	}
	if in.WorkflowJSON != "" {
		imported, err := svc.ImportWorkflow([]byte(in.WorkflowJSON))
		if err != nil {
			return Model{}, mapError(err)
		}
		in.WorkflowJSON = string(imported.Graph)
		if in.Assignments == nil {
			return Model{}, &service.ValidationError{Field: "assignments", Message: "ワークフローを替えるときは、新しいワークフローへの assignments と output_node も渡してください"}
		}
	}
	m, err := in.apply(cur.Model)
	if err != nil {
		return Model{}, err
	}
	if err := svc.UpdateModel(ctx, in.Endpoint, in.Name, localmodels.ModelInput{Endpoint: in.Endpoint, Name: in.Name, Model: m}); err != nil {
		return Model{}, mapError(err)
	}
	return h.getModel(ctx, in.ModelRef)
}

func (h handlers) deleteModel(ctx context.Context, in ModelRef) (Deleted, error) {
	svc, err := h.local()
	if err != nil {
		return Deleted{}, err
	}
	if err := svc.DeleteModel(ctx, in.Endpoint, in.Name); err != nil {
		return Deleted{}, mapError(err)
	}
	return Deleted{Name: in.Endpoint + "/" + in.Name}, nil
}
