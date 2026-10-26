package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
)

// maxUIBytes bounds the editor's format a workflow keeps.
const maxUIBytes = 8 << 20

// CreateInput is a workflow to save.
type CreateInput struct {
	Name string
	// Kind is image or video.
	Kind localcfg.Media
	// Endpoint is the ComfyUI endpoint that runs it ("": the bundled ComfyUI).
	Endpoint string
	// API is the graph in API format ("Save (API Format)", or the {"prompt": ...} POST /prompt takes).
	API []byte
	// UI is the editor's own format, to open the workflow again in the node screen (optional).
	UI string
	// Spec is the input assignment; nil lets Suggest guess it.
	Spec *localcfg.WorkflowSpec
}

// Create saves a new workflow as version 1. A name in use is ErrExists; a graph that is not in API format is
// *InvalidError. A workflow whose assignment is not complete is saved too (Ready is false and Problems say why),
// so the assignment can be set afterwards.
func (s *Service) Create(ctx context.Context, in CreateInput) (Workflow, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return Workflow{}, err
	}
	if in.Kind != localcfg.MediaImage && in.Kind != localcfg.MediaVideo {
		return Workflow{}, invalid("種類は画像（image）か動画（video）を選んでください")
	}
	endpoint, err := cleanEndpoint(in.Endpoint)
	if err != nil {
		return Workflow{}, err
	}
	graph, err := normalizeGraph(in.API)
	if err != nil {
		return Workflow{}, err
	}
	if err := checkUI(in.UI); err != nil {
		return Workflow{}, err
	}
	spec := Suggest(graph, in.Kind)
	if in.Spec != nil {
		spec = *in.Spec
		if err := s.checkSpec(in.Kind, graph, spec); err != nil {
			return Workflow{}, err
		}
	}
	rawSpec, _ := json.Marshal(spec)
	now := s.Now().UTC()
	w := comfywf.Workflow{
		ID: domain.NewID(), Name: name, Kind: string(in.Kind), Endpoint: endpoint, UIJSON: in.UI,
		APIJSON: graph, Assignment: rawSpec, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Repo.Create(ctx, w); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return Workflow{}, ErrExists
		}
		return Workflow{}, err
	}
	if err := s.refresh(ctx); err != nil {
		return Workflow{}, err
	}
	return s.view(w, s.Adapter.Config()), nil
}

// UpdateInput is what to change about a workflow; a nil field stays as it is.
type UpdateInput struct {
	Name     *string
	Endpoint *string
	// API replaces the graph.
	API []byte
	// UI replaces the editor's format.
	UI *string
	// Spec replaces the input assignment, the output node and the video settings.
	Spec *localcfg.WorkflowSpec
}

// Update changes a workflow. A change of the graph or of the assignment makes a new version when the current
// one could run (a Take may name it, so it is kept as it was); a draft that never could run is edited in
// place. A new name, endpoint or editor format never makes a version.
func (s *Service) Update(ctx context.Context, id string, in UpdateInput) (Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.Repo.Get(ctx, id)
	if err != nil {
		return Workflow{}, mapNotFound(err)
	}
	kind := localcfg.Media(w.Kind)
	wasReady := s.view(w, s.Adapter.Config()).Ready
	changed := false
	if in.Name != nil {
		if w.Name, err = cleanName(*in.Name); err != nil {
			return Workflow{}, err
		}
	}
	if in.Endpoint != nil {
		if w.Endpoint, err = cleanEndpoint(*in.Endpoint); err != nil {
			return Workflow{}, err
		}
	}
	if in.UI != nil {
		if err := checkUI(*in.UI); err != nil {
			return Workflow{}, err
		}
		w.UIJSON = *in.UI
	}
	if in.API != nil {
		graph, err := normalizeGraph(in.API)
		if err != nil {
			return Workflow{}, err
		}
		changed = changed || !sameJSON(graph, w.APIJSON)
		w.APIJSON = graph
	}
	if in.Spec != nil {
		if err := s.checkSpec(kind, w.APIJSON, *in.Spec); err != nil {
			return Workflow{}, err
		}
		raw, _ := json.Marshal(*in.Spec)
		changed = changed || !sameJSON(raw, w.Assignment)
		w.Assignment = raw
	}
	if changed && wasReady {
		w.Version++
	}
	w.UpdatedAt = s.Now().UTC()
	if err := s.Repo.Update(ctx, w); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return Workflow{}, ErrExists
		}
		return Workflow{}, mapNotFound(err)
	}
	if err := s.refresh(ctx); err != nil {
		return Workflow{}, err
	}
	return s.view(w, s.Adapter.Config()), nil
}

// Delete removes a workflow and its versions. A workflow an image purpose or a Quality Policy chooses is refused
// (*InvalidError): change the assignment first. Takes that name it keep their identifier.
func (s *Service) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.Repo.Get(ctx, id)
	if err != nil {
		return mapNotFound(err)
	}
	if used := usedBy(s.Adapter.Config(), id); len(used) > 0 {
		return invalid("ワークフロー %s は %s で使われています。先に割り当てを変えてください", w.Name, strings.Join(used, "、"))
	}
	if err := s.Repo.Delete(ctx, id); err != nil {
		return mapNotFound(err)
	}
	return s.refresh(ctx)
}

// checkSpec refuses an assignment the model check would: a node or input the graph lacks, no prompt, no output node.
func (s *Service) checkSpec(kind localcfg.Media, graph json.RawMessage, spec localcfg.WorkflowSpec) error {
	if errs := localcfg.ValidateSaved(spec.Model(kind, graph)); len(errs) > 0 {
		return invalid("入力の割り当てを保存できません: %v", errs[0])
	}
	return nil
}

func cleanEndpoint(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return BundledEndpoint, nil
	}
	if !localcfg.ValidName(name) {
		return "", invalid("接続先の名前が正しくありません")
	}
	return name, nil
}

func normalizeGraph(raw []byte) (json.RawMessage, error) {
	graph, err := localcfg.Normalize(raw)
	if err != nil {
		return nil, invalid("ワークフローを取り込めません: %v", err)
	}
	return graph, nil
}

func checkUI(ui string) error {
	switch {
	case ui == "":
		return nil
	case len(ui) > maxUIBytes:
		return invalid("ノード画面の形式のワークフローが大きすぎます")
	case !json.Valid([]byte(ui)):
		return invalid("ノード画面の形式のワークフローが JSON ではありません")
	}
	return nil
}

// Endpoints lists the registered ComfyUI endpoints a workflow can name, the bundled one first (it is listed even
// when the engine is not installed yet: a workflow imported before then runs once it is).
func (s *Service) Endpoints() []string {
	out := []string{BundledEndpoint}
	for name, e := range s.Adapter.Config().Endpoints {
		if e.Kind == localcfg.KindComfyUI && name != BundledEndpoint {
			out = append(out, name)
		}
	}
	slices.Sort(out[1:])
	return out
}
