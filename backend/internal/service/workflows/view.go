package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modelreg"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/comfywf"
)

// Workflow is a saved workflow as the screens and tools show it.
type Workflow struct {
	ID   string
	Name string
	// Kind is image or video.
	Kind localcfg.Media
	// Endpoint is the registered ComfyUI endpoint that runs it.
	Endpoint string
	// Version is the current version; ModelID the identifier ("comfy:<id>@v<N>") a Take made by it records.
	Version   int
	ModelID   string
	CreatedAt time.Time
	UpdatedAt time.Time
	// Spec is the input assignment, the output node and the video settings.
	Spec localcfg.WorkflowSpec
	// Ready: the current version can run (it is a choice for image purposes, Quality Policies and a Shot's lock).
	// When false, Problems say why (English, from the same check as a registered model).
	Ready    bool
	Problems []string
	// HasUI: the editor's format is saved, so the workflow can be opened again in the node screen.
	HasUI bool
	// UsedBy are the image purposes and Quality Policies that choose it.
	UsedBy []string
}

// Detail is a Workflow with its graph.
type Detail struct {
	Workflow
	// UIJSON is the editor's format ("" when only the API format was imported).
	UIJSON string
	// APIJSON is the graph that runs.
	APIJSON json.RawMessage
	// Nodes lists the nodes and inputs of the graph, for the assignment editor.
	Nodes []localcfg.NodeInfo
}

func (s *Service) view(w comfywf.Workflow, cfg modeladapter.Config) Workflow {
	spec, err := localcfg.ParseSpec(w.Assignment)
	v := Workflow{
		ID: w.ID, Name: w.Name, Kind: localcfg.Media(w.Kind), Endpoint: w.Endpoint, Version: w.Version,
		ModelID: modelreg.WorkflowID(w.ID, w.Version), CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, Spec: spec, HasUI: w.UIJSON != "",
	}
	if err != nil {
		v.Problems = []string{err.Error()}
		return v
	}
	for _, e := range localcfg.ValidateSaved(spec.Model(v.Kind, w.APIJSON)) {
		v.Problems = append(v.Problems, e.Error())
	}
	v.Ready = len(v.Problems) == 0
	v.UsedBy = usedBy(cfg, w.ID)
	return v
}

// List returns every workflow by name.
func (s *Service) List(ctx context.Context) ([]Workflow, error) {
	rows, err := s.Repo.List(ctx)
	if err != nil {
		return nil, err
	}
	cfg := s.Adapter.Config()
	out := make([]Workflow, len(rows))
	for i, w := range rows {
		out[i] = s.view(w, cfg)
	}
	return out, nil
}

// Get returns one workflow with its graph, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (Detail, error) {
	w, err := s.Repo.Get(ctx, id)
	if err != nil {
		return Detail{}, mapNotFound(err)
	}
	d := Detail{Workflow: s.view(w, s.Adapter.Config()), UIJSON: w.UIJSON, APIJSON: w.APIJSON}
	d.Nodes, _ = localcfg.Inspect(w.APIJSON)
	return d, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// Version is what ran as one version of a workflow.
type Version struct {
	Version   int
	CreatedAt time.Time
	Current   bool
	// ModelID is the identifier a Take of this version records.
	ModelID string
	Spec    localcfg.WorkflowSpec
	APIJSON json.RawMessage
}

// Versions lists every version of the workflow, newest first.
func (s *Service) Versions(ctx context.Context, id string) ([]Version, error) {
	w, err := s.Repo.Get(ctx, id)
	if err != nil {
		return nil, mapNotFound(err)
	}
	all, err := s.Repo.Versions(ctx)
	if err != nil {
		return nil, err
	}
	var out []Version
	for _, v := range all {
		if v.WorkflowID != id {
			continue
		}
		spec, _ := localcfg.ParseSpec(v.Assignment)
		out = append(out, Version{Version: v.Version, CreatedAt: v.CreatedAt, Current: v.Version == w.Version, ModelID: modelreg.WorkflowID(id, v.Version), Spec: spec, APIJSON: v.APIJSON})
	}
	slices.Reverse(out)
	return out, nil
}

// usedBy lists where the settings choose a version of the workflow (routes follow the latest version).
func usedBy(cfg modeladapter.Config, workflowID string) []string {
	is := func(m domain.ModelName) bool {
		id, _, ok := modelreg.ParseWorkflowID(string(m))
		return ok && id == workflowID
	}
	var used []string
	for _, p := range slices.Sorted(maps.Keys(cfg.Images)) {
		r := cfg.Images[p]
		if is(r.Primary.Model) {
			used = append(used, "images."+string(p)+".primary")
		}
		if is(r.Fallback.Model) {
			used = append(used, "images."+string(p)+".fallback")
		}
	}
	for _, q := range slices.Sorted(maps.Keys(cfg.Policies)) {
		r := cfg.Policies[q]
		if is(r.Primary.Model) {
			used = append(used, "policies."+string(q)+".primary")
		}
		if is(r.Fallback.Model) {
			used = append(used, "policies."+string(q)+".fallback")
		}
	}
	for _, q := range slices.Sorted(maps.Keys(cfg.Routing.Candidates)) {
		if slices.ContainsFunc(cfg.Routing.Candidates[q], func(t modeladapter.VideoTarget) bool { return is(t.Model) }) {
			used = append(used, "routing.candidates."+string(q))
		}
	}
	return used
}

// sameJSON reports whether two JSON documents are the same once compacted.
func sameJSON(a, b []byte) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// MaxNameRunes is the longest workflow name.
const MaxNameRunes = 100

// cleanName trims a workflow name and refuses an empty, too long or multi-line one.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", invalid("ワークフローの名前を入力してください")
	case len([]rune(name)) > MaxNameRunes:
		return "", invalid("ワークフローの名前は %d 文字以内にしてください", MaxNameRunes)
	case strings.ContainsAny(name, "\r\n\t\x00"):
		return "", invalid("ワークフローの名前に改行やタブは使えません")
	}
	return name, nil
}

// PinID is the identifier a Shot's Video Model lock takes for version of the workflow ("comfy:<id>@v<N>"). The
// version must exist and be able to run, and be the current one: an older version is offered only to the Shots that
// are pinned to it already (they keep it when the workflow is edited).
func (s *Service) PinID(ctx context.Context, id string, version int) (string, error) {
	cur, err := s.Repo.Get(ctx, id)
	if err != nil {
		return "", mapNotFound(err)
	}
	if _, err := s.Repo.GetVersion(ctx, id, version); err != nil {
		return "", mapNotFound(err)
	}
	if version != cur.Version {
		return "", invalid("Shot に固定できるのは現在の版（v%d）だけです。古い版に固定済みの Shot は、そのまま使えます", cur.Version)
	}
	modelID := modelreg.WorkflowID(id, version)
	if _, ok := s.Adapter.Config().Workflows[modelID]; !ok {
		return "", s.notRunnable(ctx, id, version)
	}
	return modelID, nil
}
