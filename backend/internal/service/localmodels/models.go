package localmodels

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// ModelInput is a model to create or update on an endpoint. Endpoint and Name identify it for good (they are
// the identifier in take.model_name: "<kind>:<endpoint>/<name>"); an update may only repeat them.
type ModelInput struct {
	Endpoint string
	Name     string
	Model    localcfg.Model
}

// CreateModel defines a model on a registered endpoint. The model is checked as at start-up (a workflow whose
// assignments point to nodes and inputs that do not exist, a video model without durations, ... are
// *InvalidError); a name in use is ErrExists.
func (s *Service) CreateModel(_ context.Context, in ModelInput) error {
	return s.change(func(st *state, _ modeladapter.Config) error {
		if _, ok := st.endpoints[in.Endpoint]; !ok {
			return ErrNotFound
		}
		key := localcfg.Key(in.Endpoint, in.Name)
		if _, ok := st.models[key]; ok {
			return ErrExists
		}
		if !localcfg.ValidName(in.Name) {
			return invalid("モデルの名前は英小文字・数字・「.」「_」「-」の 1〜63 文字にしてください")
		}
		in.Model = normalized(in.Model)
		if err := firstError(localcfg.ValidateModel(key, in.Model, st.endpoints)); err != nil {
			return err
		}
		st.models[key] = in.Model
		return nil
	})
}

// UpdateModel replaces the definition of the model called name on endpoint. A different Endpoint or Name in
// the input is refused: a rename is a delete and a create.
func (s *Service) UpdateModel(_ context.Context, endpoint, name string, in ModelInput) error {
	return s.change(func(st *state, _ modeladapter.Config) error {
		key := localcfg.Key(endpoint, name)
		if _, ok := st.models[key]; !ok {
			return ErrNotFound
		}
		if (in.Endpoint != "" && in.Endpoint != endpoint) || (in.Name != "" && in.Name != name) {
			return invalid("モデルの名前と接続先は変更できません。削除して作り直してください（Take とコスト台帳に残る識別子が名前でできているため）")
		}
		in.Model = normalized(in.Model)
		if err := firstError(localcfg.ValidateModel(key, in.Model, st.endpoints)); err != nil {
			return err
		}
		st.models[key] = in.Model
		return nil
	})
}

// DeleteModel removes a model. A model chosen in an image purpose or a Quality Policy is refused
// (*InvalidError): change the assignment first.
func (s *Service) DeleteModel(_ context.Context, endpoint, name string) error {
	return s.change(func(st *state, cfg modeladapter.Config) error {
		key := localcfg.Key(endpoint, name)
		if _, ok := st.models[key]; !ok {
			return ErrNotFound
		}
		if routes := usedBy(cfg, localcfg.ID(st.endpoints[endpoint].Kind, endpoint, name)); len(routes) > 0 {
			return invalid("このモデルは %s で使われています。先に割り当てを変えてください", strings.Join(routes, "、"))
		}
		delete(st.models, key)
		return nil
	})
}

// normalized compacts the workflow graph, so that the file does not hold what the editor indented.
func normalized(m localcfg.Model) localcfg.Model {
	if m.Workflow != nil {
		if g, err := localcfg.Normalize(m.Workflow.Graph); err == nil {
			w := *m.Workflow
			w.Graph = g
			m.Workflow = &w
		}
	}
	m.Model = strings.TrimSpace(m.Model)
	return m
}

// ImportedWorkflow is a workflow read from JSON: the graph to store and its nodes with their inputs, for the
// screen where the user says which node input each value goes to.
type ImportedWorkflow struct {
	Graph json.RawMessage
	Nodes []localcfg.NodeInfo
}

// ImportWorkflow reads a workflow saved with "Save (API Format)" in ComfyUI (or the {"prompt": ...} object
// POST /prompt takes). The editor's own format is refused with a hint. Nothing is saved: the graph goes into
// a ModelInput with the assignments the user sets (or the ones of a Template).
func (s *Service) ImportWorkflow(raw []byte) (ImportedWorkflow, error) {
	graph, err := localcfg.Normalize(raw)
	if err != nil {
		return ImportedWorkflow{}, invalid("ワークフローを取り込めません: %v", err)
	}
	nodes, err := localcfg.Inspect(graph)
	if err != nil {
		return ImportedWorkflow{}, invalid("ワークフローを取り込めません: %v", err)
	}
	return ImportedWorkflow{Graph: graph, Nodes: nodes}, nil
}

// Templates are the built-in workflows (txt2img, img2img, txt2video, img2video; core nodes only).
func (s *Service) Templates() []localcfg.Template { return localcfg.Templates() }
