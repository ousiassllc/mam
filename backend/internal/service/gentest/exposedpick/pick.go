// Package exposedpick is how a person chooses the "画面に出す値" of a ComfyUI workflow (Issue #977): the inputs of the nodes of the model's graph that the
// generation test screen offers as fields of their own (localcfg/exposed). It lists the nodes and inputs of a model's graph with whether each can be chosen
// (an input another node feeds, one an assignment feeds, or one that is not a number, a text or a choice cannot, and the reason is given), with the range or the
// choices the ComfyUI server says the input takes (GET /object_info) where it can be asked, and it saves the chosen ones into the place the model lives: a workflow
// saved in mam, or a model of a registered endpoint (the models the model manager installs and the imported workflows are of this kind).
package exposedpick

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/exposed"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
)

// Picker chooses the exposed inputs of the models of the generation test. Workflows and Local are where a model is saved (either may be nil: the models that
// live there cannot be changed).
type Picker struct {
	Gentest   *gentest.Service
	Workflows *workflows.Service
	Local     *localmodels.Service
}

// Input is an exposed input (the handlers of the screen name it through here).
type Input = exposed.Input

// Model is an image model that runs a ComfyUI workflow, with how many of its inputs are exposed now.
type Model struct {
	ID, Label string
	Exposed   int
	// Editable is false for a model whose place cannot be changed (a settings file pinned with --model-config, a service that is not there).
	Editable bool
}

// Row is one input of one node of the graph. Reason says why it cannot be chosen ("" when it can). Chosen is the exposed input when it is one now.
type Row struct {
	Node, Class, Input, Value string
	Type                      exposed.Type
	Min, Max                  *float64
	Options                   []string
	Reason                    string
	Chosen                    *exposed.Input
}

// Key is "<node>.<input>".
func (r Row) Key() string { return r.Node + "." + r.Input }

// Models lists the image models that run a workflow, by label.
func (p Picker) Models(ctx context.Context) []Model {
	var out []Model
	for _, m := range p.Gentest.Models(ctx, gentest.MediaImage) {
		if lm, _, ok := p.Gentest.Adapter.Config().LocalModel(domain.ModelName(m.ID)); ok && lm.Workflow != nil && !lm.IsUpscale() && !lm.IsVideoEnhance() {
			out = append(out, Model{ID: m.ID, Label: m.Label, Exposed: len(lm.Workflow.ExposedInputs), Editable: p.where(m.ID) != nil})
		}
	}
	slices.SortStableFunc(out, func(a, b Model) int { return strings.Compare(a.Label, b.Label) })
	return out
}

func (p Picker) workflowOf(id string) (localcfg.Workflow, error) {
	lm, _, ok := p.Gentest.Adapter.Config().LocalModel(domain.ModelName(id))
	if !ok || lm.Workflow == nil {
		return localcfg.Workflow{}, gterr.Invalid("モデル %q は ComfyUI のワークフローではありません", id)
	}
	return *lm.Workflow, nil
}

// Rows lists the inputs of the model's graph with what each can be, by node and input. The server is asked for the types, ranges and choices when it
// answers without being started (a stopped bundled ComfyUI is not started for this); without it they are read from the values the graph holds.
func (p Picker) Rows(ctx context.Context, modelID string) ([]Row, error) {
	w, err := p.workflowOf(modelID)
	if err != nil {
		return nil, err
	}
	nodes, err := localcfg.Inspect(w.Graph)
	if err != nil {
		return nil, gterr.Invalid("グラフを読めません: %v", err)
	}
	g, _ := localcfg.ParseGraph(w.Graph)
	var info map[string]classInfo
	if p.Workflows != nil {
		endpoint, _ := localcfg.EndpointOf(modelID)
		if raw, err := p.Workflows.ObjectInfo(ctx, endpoint, false); err == nil {
			_ = json.Unmarshal(raw, &info)
		}
	}
	var rows []Row
	for _, n := range nodes {
		for _, in := range n.Inputs {
			row := Row{Node: n.ID, Class: n.Class, Input: in.Name, Value: in.Value}
			row.Reason = p.classify(&row, g[n.ID].Inputs[in.Name], info[n.Class].spec(in.Name), w, in.Link)
			if p.Gentest.HideChoices != nil && len(row.Options) > 0 {
				row.Options = p.Gentest.HideChoices(row.Options, in.Value) // the choices the closed age gate hides are not offered
			}
			if i := slices.IndexFunc(w.ExposedInputs, func(e exposed.Input) bool { return e.Node == n.ID && e.Input == in.Name }); i >= 0 {
				row.Chosen = &w.ExposedInputs[i]
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// classify fills the type and the range of the row and returns why it cannot be chosen.
func (Picker) classify(row *Row, raw json.RawMessage, spec inputSpec, w localcfg.Workflow, linked bool) string {
	switch {
	case linked:
		return "ほかのノードがつないでいる入力のため選べません"
	case slices.ContainsFunc(w.Assignments, func(a localcfg.Assignment) bool { return a.Node == row.Node && a.Input == row.Input }):
		return "mam が値を入れる入力（プロンプト・シード・サイズ・ステップ数など）のため選べません。こだわりの詳細設定にあります"
	}
	var v any
	_ = json.Unmarshal(raw, &v)
	switch x := v.(type) {
	case float64:
		row.Type = exposed.Float
		if spec.kind == "INT" || (spec.kind == "" && x == float64(int64(x)) && !strings.ContainsAny(string(raw), ".eE")) {
			row.Type = exposed.Int
		}
		if spec.kind == "BOOLEAN" || spec.kind == "STRING" {
			return "数値の入力ではありません"
		}
		row.Min, row.Max = spec.min, spec.max
		if row.Min == nil || row.Max == nil {
			lo, hi := guessRange(x)
			row.Min, row.Max = &lo, &hi
		}
	case string:
		row.Type = exposed.String
		if len(spec.options) > 0 {
			row.Type, row.Options = exposed.Enum, spec.options
		}
	default:
		return "数値・文字・選択肢の入力だけ選べます（真偽値・リスト・つなぎは選べません）"
	}
	return ""
}

// guessRange is a range around a value when the server cannot say one (the person edits it): from 0, or from the negative of the value, to a few times it.
func guessRange(x float64) (lo, hi float64) {
	hi = max(10, x*4)
	if x < 0 {
		return x * 4, 10
	}
	return 0, hi
}

// Save makes inputs the exposed inputs of the model (the others are dropped; none clears them). The model is checked as when it is saved anywhere
// (the inputs must exist and be literal, not assigned, and hold a value within the range).
func (p Picker) Save(ctx context.Context, modelID string, inputs []exposed.Input) error {
	where := p.where(modelID)
	if where == nil {
		return gterr.Invalid("このモデルの「画面に出す値」は、ここでは変えられません（設定ファイルが固定されているか、モデルが保存されていません）")
	}
	if inputs == nil {
		inputs = []exposed.Input{}
	}
	return where(ctx, inputs)
}

type saver func(ctx context.Context, inputs []exposed.Input) error

// where is how the model is saved: nil when it cannot be.
func (p Picker) where(modelID string) saver {
	if id, _, ok := modelreg.ParseWorkflowID(modelID); ok && p.Workflows != nil {
		return func(ctx context.Context, inputs []exposed.Input) error {
			d, err := p.Workflows.Get(ctx, id)
			if err != nil {
				return gterr.NotFound(err)
			}
			spec := d.Spec
			spec.ExposedInputs = inputs
			_, err = p.Workflows.Update(ctx, id, workflows.UpdateInput{Spec: &spec})
			return refusal(err)
		}
	}
	if _, endpoint, name, ok := localcfg.ParseID(modelID); ok && p.Local != nil {
		return func(ctx context.Context, inputs []exposed.Input) error {
			view, err := p.Local.View()
			if err != nil {
				return err
			}
			i := slices.IndexFunc(view.Models, func(m localmodels.ModelView) bool { return m.Endpoint == endpoint && m.Name == name })
			if i < 0 {
				return gterr.NotFound(localmodels.ErrNotFound)
			}
			m := view.Models[i].Model
			wf := *m.Workflow
			wf.ExposedInputs = inputs
			m.Workflow = &wf
			return refusal(p.Local.UpdateModel(ctx, endpoint, name, localmodels.ModelInput{Endpoint: endpoint, Name: name, Model: m}))
		}
	}
	return nil
}

// refusal turns the refusals of the services that save a model into the generation test's.
func refusal(err error) error {
	var in *workflows.InvalidError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &in):
		return gterr.Invalid("%s", in.Message)
	}
	return err
}

// classInfo is what GET /object_info says of one node class.
type classInfo struct {
	Input struct {
		Required map[string]json.RawMessage `json:"required"`
		Optional map[string]json.RawMessage `json:"optional"`
	} `json:"input"`
}

// inputSpec is the declaration of one input: kind is INT, FLOAT, STRING, BOOLEAN or "" (unknown, or a choice: options).
type inputSpec struct {
	kind     string
	min, max *float64
	options  []string
}

func (c classInfo) spec(input string) inputSpec {
	raw, ok := c.Input.Required[input]
	if !ok {
		if raw, ok = c.Input.Optional[input]; !ok {
			return inputSpec{}
		}
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return inputSpec{}
	}
	var s inputSpec
	if json.Unmarshal(parts[0], &s.kind) != nil {
		_ = json.Unmarshal(parts[0], &s.options) // [[choices], {...}]
	}
	if len(parts) > 1 {
		var extra struct {
			Min *float64 `json:"min"`
			Max *float64 `json:"max"`
		}
		_ = json.Unmarshal(parts[1], &extra)
		s.min, s.max = extra.Min, extra.Max
	}
	return s
}

// Build makes an exposed input from what the form of the picker holds for a row: the label and, for a number, the range as text.
func Build(r Row, label, min, max string) (exposed.Input, error) {
	in := exposed.Input{Node: r.Node, Input: r.Input, Type: r.Type, Label: strings.TrimSpace(label), Options: r.Options}
	if r.Type == exposed.Int || r.Type == exposed.Float {
		lo, err1 := strconv.ParseFloat(strings.TrimSpace(min), 64)
		hi, err2 := strconv.ParseFloat(strings.TrimSpace(max), 64)
		if err1 != nil || err2 != nil {
			return in, gterr.Invalid("%s: 下限と上限を数字で入力してください", r.Key())
		}
		in.Min, in.Max = &lo, &hi
	}
	return in, nil
}
