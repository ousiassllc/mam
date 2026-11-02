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
	e.Dest = Dest{Screen: ScreenModelManager}
	mm := r.ModelManager
	if mm == nil {
		return nil
	}
	e.Subject = installSubject(mm, p.Target)
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

// installSubject is the name of what an install works on: "engine", "model:<catalog id>" or "hf:<repo>@<commit>/<path>". A model
// that is hidden (adult, while the age confirmation is off) or not in the catalog has no name here.
func installSubject(mm *modelmanager.Service, target string) string {
	if target == "engine" {
		return "ComfyUI エンジン"
	}
	if id, ok := strings.CutPrefix(target, "model:"); ok {
		if m, err := mm.Model(id); err == nil {
			return m.Name
		}
		return ""
	}
	if rest, ok := strings.CutPrefix(target, "hf:"); ok {
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
	e.Subject = modelNames(b.Results)
	e.State = State(b.State)
	if b.Error != "" {
		e.Error = logging.Redact(b.Error)
	}
	if b.Total == 0 {
		return nil
	}
	percent := b.Done * 100
	if v, ok := liveView(e.ID); ok && e.State == Running {
		percent += v.Percent
	}
	e.Progress = &Progress{Percent: percent / b.Total, Text: fmt.Sprintf("%d / %d 件", b.Done, b.Total)}
	return nil
}

// modelNames lists the models of the results of a batch in the order they were planned, each once.
func modelNames(results []record.ResultView) string {
	var names []string
	seen := map[string]bool{}
	for _, res := range results {
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
		e.Progress = &Progress{Percent: v.Percent, Text: fmt.Sprintf("ステップ %d / %d", v.Step, v.MaxStep)}
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
