package joblist

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/logging"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/ollamapull"
)

// reader is one reading of the list: what it learned of a Project is kept for the jobs after it.
type reader struct {
	*Service
	projects map[string]*string // the name of the Projects looked up, nil for one that is gone
}

// maxModels is how many models of a generation test are named before "ほか".
const maxModels = 3

// install is a model install: it leads to the model manager, works on the engine or a model, and tells how far the download is
// while it runs (the tracker of the model manager keeps that in memory: a job from before a restart has none).
func (r *reader) install(ctx context.Context, e *Entry, p payload) error {
	e.Dest = Dest{Screen: ScreenModelManager, Category: installCategory(p.Target)}
	mm := r.ModelManager
	if mm == nil {
		return nil
	}
	e.Subject = installSubject(mm, p)
	if e.State != Running {
		return nil
	}
	st, err := mm.JobStatus(ctx, e.ID)
	if errors.Is(err, modelmanager.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	e.Progress = &Progress{Percent: st.Progress.Percent, Text: st.Progress.Message}
	return nil
}

// llmRuntimeTargets are the install targets of the runtimes of a language model: their page is "ローカル LLM", not "ローカル音声・口パク" (#1104).
var llmRuntimeTargets = map[string]bool{"runtime:llama-cpp": true, "runtime:ollama": true}

// installCategory is the page of the model manager that works on the target of an install: "engine", "model:<id>", "runtime:<id>", "node:<id>", "ollama:<name>:<tag>" or "hf:…" ("" for
// a target that says nothing: the overview).
func installCategory(target string) string {
	switch {
	case target == "engine":
		return CategoryComfyUI
	case strings.HasPrefix(target, "model:"):
		return CategoryImageVideo
	case llmRuntimeTargets[target]:
		return CategoryLocalLLM
	case strings.HasPrefix(target, "runtime:"):
		return CategoryLocalRuntimes
	case strings.HasPrefix(target, "node:"):
		return CategoryCustomNodes
	case strings.HasPrefix(target, ollamapull.TargetPrefix):
		return CategoryLocalLLM // a model fetched by the Ollama of mam (#1105)
	case strings.HasPrefix(target, "hf:"):
		return CategoryHFExternal
	}
	return ""
}

// installSubject is the name of what an install works on: "engine", "model:<catalog id>" or "hf:<repo>@<commit>/<path>". A model
// that is hidden (adult, while the age confirmation is off: the catalog's model, a Hugging Face file of an NFAA repository, which the install
// itself refuses on the same flag: hfplan.Payload.NFAA) or not in the catalog has no name here.
func installSubject(mm *modelmanager.Service, p payload) string {
	if p.Target == "engine" {
		return "ComfyUI エンジン"
	}
	if id, ok := strings.CutPrefix(p.Target, "model:"); ok {
		if m, err := mm.Model(id); err == nil {
			return m.Name
		}
		return ""
	}
	if name, ok := strings.CutPrefix(p.Target, ollamapull.TargetPrefix); ok {
		return "Ollama: " + name
	}
	if rest, ok := strings.CutPrefix(p.Target, "hf:"); ok {
		if p.HF != nil && p.HF.NFAA && !mm.AdultContentEnabled() {
			return ""
		}
		repo, _, _ := strings.Cut(rest, "@")
		return repo
	}
	return ""
}

// gentest is a batch of the generation test: it leads to the generation test, names the models of the batch and counts its results.
// The state is the batch's (it knows a cancelled one), and a step of a ComfyUI call that runs counts into the percentage.
func (r *reader) gentest(ctx context.Context, e *Entry, p payload) error {
	e.Dest = Dest{Screen: ScreenGenerationTest}
	g := r.Gentest
	if g == nil || p.BatchID == "" {
		return nil
	}
	b, err := g.Batch(ctx, p.BatchID, true)
	if errors.Is(err, gterr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	e.Subject = modelNames(b.Results, g.Hidden)
	e.State = State(b.State)
	if b.Error != "" {
		e.Error = logging.Redact(b.Error)
	}
	if b.Total == 0 {
		return nil
	}
	percent, text := b.Done*100, fmt.Sprintf("%d / %d 件", b.Done, b.Total)
	if v, ok := liveView(e.ID); ok && e.State == Running {
		percent += v.Percent
		text += "・" + liveText(v)
	}
	e.Progress = &Progress{Percent: percent / b.Total, Text: text}
	return nil
}

// modelNames lists the models of the results of a batch in the order they were planned, each once. A model that hidden says is hidden (the
// adult models while the age gate is closed; nil hides none) has no name here.
func modelNames(results []record.ResultView, hidden func(modelID string) bool) string {
	var names []string
	seen := map[string]bool{}
	for _, res := range results {
		if hidden != nil && hidden(res.Model) {
			continue
		}
		name := res.ModelLabel
		if name == "" {
			name = res.Model
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) > maxModels {
		return strings.Join(names[:maxModels], "、") + fmt.Sprintf(" ほか %d モデル", len(names)-maxModels)
	}
	return strings.Join(names, "、")
}

// shot is the work on a Shot (generation, Judge, Repair): it leads to the Scene / Shot screen of the Project of the Shot, and a
// generation or a Repair that runs on the local ComfyUI says which step it is at.
func (r *reader) shot(ctx context.Context, e *Entry, shotID string) error {
	if shotID == "" {
		return nil
	}
	ref, err := r.Shots.GetShot(ctx, shotID)
	if missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := r.project(ctx, e, ref.ProjectID, ScreenProjectScenes); err != nil {
		return err
	}
	if v, ok := liveView(shotID); ok && e.State == Running {
		e.Progress = &Progress{Percent: v.Percent, Text: liveText(v)}
	}
	return nil
}

// project names the Project as the subject of the job and leads to its screen; a Project that is gone leaves both empty.
func (r *reader) project(ctx context.Context, e *Entry, projectID string, screen Screen) error {
	if projectID == "" {
		return nil
	}
	name, ok := r.projects[projectID]
	if !ok {
		p, err := r.Projects.Get(ctx, projectID)
		switch {
		case err == nil:
			name = &p.Name
		case !missing(err):
			return err
		}
		r.projects[projectID] = name
	}
	if name == nil {
		return nil
	}
	e.Subject, e.Dest = *name, Dest{Screen: screen, ProjectID: projectID}
	return nil
}

// liveView is how far the ComfyUI call of an owner (a Shot or a job) is, when it has reported a step.
func liveView(owner string) (*live.View, bool) {
	p, ok := live.Default.ByOwner(owner)
	if !ok {
		return nil, false
	}
	v := p.View()
	return v, v != nil
}
