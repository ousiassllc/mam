package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/agent/promptplanner"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// errInvalidPlan is the error class of an AI answer that cannot be used.
var errInvalidPlan = planning.ErrInvalidPlan

var ratioRe = regexp.MustCompile(`(\d+)\s*[:：]\s*(\d+)`)

// aspectRatio reads the Brief's aspect ratio ("9:16", "縦 9:16") and falls back to 16:9. Only
// ratios the image and video models accept are returned.
func aspectRatio(brief domain.Brief) string {
	if m := ratioRe.FindStringSubmatch(brief.AspectRatio); m != nil {
		switch r := m[1] + ":" + m[2]; r {
		case "16:9", "9:16", "1:1":
			return r
		}
	}
	return "16:9"
}

// recordedRequest is what prompt_version.model_adapter_output keeps of a request: the prompt of the
// model (the images are only counted).
type recordedRequest struct {
	Prompt          string  `json:"prompt"`
	NegativePrompt  string  `json:"negative_prompt,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
	Resolution      string  `json:"resolution"`
	AspectRatio     string  `json:"aspect_ratio"`
	ReferenceImages int     `json:"reference_images"`
	FirstFrame      bool    `json:"first_frame"`
}

// adapterRecord is the content of prompt_version.model_adapter_output.
type adapterRecord struct {
	Policy      domain.QualityPolicy     `json:"quality_policy"`
	Takes       int                      `json:"takes"`
	Primary     modeladapter.VideoTarget `json:"primary"`
	Fallback    modeladapter.VideoTarget `json:"fallback"`
	CostPerTake float64                  `json:"estimated_cost_per_take"`
	Reasons     []string                 `json:"reasons,omitempty"`
	// Candidate is the strategy of a Prompt Candidate (empty for a plain generation).
	Candidate domain.CandidateStrategy             `json:"candidate_strategy,omitempty"`
	Requests  map[domain.ModelName]recordedRequest `json:"requests"`
}

// generate runs Prompt Autopilot and the Model Adapter for a Shot: it writes the Canonical Shot
// Spec, decides the strategy, and makes the Takes.
func (o *Orchestrator) generate(ctx context.Context, ref repository.ShotRef, instruction string, preserve []domain.SpecPart) (Progress, error) {
	shot := ref.Shot
	sc, err := o.loadShotContext(ctx, ref, instruction)
	if err != nil {
		return Progress{}, err
	}
	policy, brief, references := sc.policy, sc.brief, sc.references
	locks, err := o.gens.ShotLocks(ctx, shot.ID)
	if err != nil {
		return Progress{}, err
	}

	// Prompt Autopilot (F-06). An existing spec is kept unless the user gave a new direction, and
	// then the LOCKed parts and the parts the director asked to preserve are carried over as they
	// are (F-05, UC-2). With Prompt LOCKed the spec is never rewritten.
	current, hasSpec := shot.CanonicalSpec()
	spec := current
	var noteRefs []domain.NoteRef // the notes the Planner was given; none when the spec is kept
	if !hasSpec || (instruction != "" && !locks.Locked(domain.ParamPrompt)) {
		in := sc.plannerInput(ref, instruction)
		if hasSpec {
			in.Current = &current
			in.Fixed = slices.Concat(locks.LockedParts(), preserve)
			in.Manual = locks.PartsWith(domain.LockManual)
		}
		if spec, err = o.planner.Plan(ctx, in); err != nil {
			return Progress{}, err
		}
		noteRefs = domain.NoteRefs(sc.notes)
	}

	// Generation strategy: Shot Spec duration, cost and model capabilities.
	budget, err := o.gens.Budget(ctx, ref.ProjectID, shot.ID)
	if err != nil {
		return Progress{}, err
	}
	strategy, err := o.decide(ctx, policy.Quality, shot, budget)
	if err != nil {
		return Progress{}, err
	}
	job, err := o.videoJob(ctx, ref.ProjectID, spec, brief, strategy.DurationSeconds, references)
	if err != nil {
		return Progress{}, err
	}
	record, err := recordOf(strategy, job, "")
	if err != nil {
		return Progress{}, err
	}
	pv, err := o.gens.SavePromptedSpec(ctx, shot.ID, spec, record, noteRefs)
	if err != nil {
		return Progress{}, err
	}

	// Takes (F-12). They run at the same time; the Model Adapter limits calls per vendor.
	o.notify(Progress{ShotID: shot.ID, Action: ActionGenerate, Stage: StageGenerating, Message: "動画を生成しています", TakesTotal: strategy.Takes})
	var made counter
	errs := parallel(strategy.Takes, func(i int) error {
		takeID, err := o.makeTake(ctx, ref, pv, strategy, job, domain.TakeLineage{WorldStateVersion: sc.world.Version, References: references}, fmt.Sprintf("Shot %d · Take %d", shot.OrderIndex+1, i+1), policy.Effective().MaxGenerationTime, "")
		if err != nil {
			return err
		}
		o.notify(Progress{ShotID: shot.ID, Action: ActionGenerate, Stage: StageGenerating, Message: "動画を生成しています", TakesDone: made.add(takeID), TakesTotal: strategy.Takes})
		o.judgeTake(ctx, takeID)
		return nil
	})
	done := made.value()
	if done == 0 {
		return Progress{}, errors.Join(errs...)
	}
	if err := errors.Join(errs...); err != nil {
		o.logger.Warn("some takes failed", "shot_id", shot.ID, "made", done, "wanted", strategy.Takes, "err", err)
	}
	o.updateWorldState(ctx, ref, spec, made.first())
	return Progress{TakesDone: done, TakesTotal: strategy.Takes}, nil
}

// shotContext is what planning a Shot needs besides the Shot itself.
type shotContext struct {
	policy     domain.ProjectPolicy
	brief      domain.Brief
	world      domain.WorldContext
	previous   *domain.ShotSpec
	references []domain.Reference
	// notes are the production notes the Prompt Planner is given (spec/spec.md §9.4).
	notes []domain.NoteView
}

// loadShotContext reads the Policy, Brief, World State, neighbouring spec, Reference Pack and notes of
// the Shot; instruction is the user's extra direction, which the choice of notes takes into account.
func (o *Orchestrator) loadShotContext(ctx context.Context, ref repository.ShotRef, instruction string) (shotContext, error) {
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return shotContext{}, err
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return shotContext{}, err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID)
	if err != nil {
		return shotContext{}, err
	}
	shots, err := o.gens.ListProjectShots(ctx, ref.ProjectID)
	if err != nil {
		return shotContext{}, err
	}
	notes, err := o.shotNotes(ctx, ref, instruction)
	if err != nil {
		return shotContext{}, err
	}
	return shotContext{policy: policy, brief: brief, world: world, previous: previousSpec(shots, ref.Shot.ID), references: ref.Shot.References(), notes: notes}, nil
}

// shotNotes chooses the production notes of the Project the Prompt Planner is given for the Shot: the
// pinned ones, then the ones that match the Scene, the Shot and the instruction, within the context
// budget (spec/spec.md §9.4). Archived notes are never chosen.
func (o *Orchestrator) shotNotes(ctx context.Context, ref repository.ShotRef, instruction string) ([]domain.NoteView, error) {
	if o.notes == nil {
		return nil, nil
	}
	all, err := o.notes.Context(ctx, ref.ProjectID)
	if err != nil {
		return nil, err
	}
	query := strings.Join([]string{ref.Scene.Title, ref.Scene.Objective, ref.Shot.Objective, instruction}, "\n")
	return domain.SelectNotes(all, query, domain.NoteContextBudgetRunes), nil
}

// plannerInput is the input of the Prompt Planner for the Shot with the user's extra direction.
func (c shotContext) plannerInput(ref repository.ShotRef, instruction string) promptplanner.Input {
	return promptplanner.Input{
		Brief: c.brief, Template: c.policy.Template, Scene: ref.Scene, Shot: ref.Shot,
		Instruction: instruction, World: c.world, Previous: c.previous, References: c.references, Notes: c.notes,
	}
}

// previousSpec is the Canonical Shot Spec of the Shot before shotID in story order.
func previousSpec(shots []repository.ShotRef, shotID string) *domain.ShotSpec {
	var prev *domain.ShotSpec
	for _, s := range shots {
		if s.Shot.ID == shotID {
			return prev
		}
		if spec, ok := s.Shot.CanonicalSpec(); ok {
			prev = &spec
		}
	}
	return nil
}

// videoJob assembles the model-independent video request of the Shot: the spec, the Keyframe as
// first frame and the other references in priority order.
func (o *Orchestrator) videoJob(ctx context.Context, projectID string, spec domain.ShotSpec, brief domain.Brief, seconds float64, refs []domain.Reference) (modeladapter.VideoJob, error) {
	job := modeladapter.VideoJob{Spec: spec, DurationSeconds: seconds, AspectRatio: aspectRatio(brief)}
	for _, r := range refs {
		asset, err := o.assets.Get(ctx, projectID, r.AssetID)
		if errors.Is(err, repository.ErrNotFound) {
			continue // the Reference Pack can be rebuilt; a missing image is not fatal
		}
		if err != nil {
			return job, err
		}
		data, err := o.files.read(asset)
		if err != nil {
			o.logger.Warn("reference image unavailable", "asset_id", r.AssetID, "err", err)
			continue
		}
		in := modeladapter.Input{Data: data, MimeType: asset.MimeType}
		if r.Kind == domain.RefKeyframe {
			job.FirstFrame = &in
		} else {
			job.References = append(job.References, in)
		}
	}
	return job, nil
}

// recordOf renders the job for both models of the strategy and returns the JSON kept in
// prompt_version.model_adapter_output.
func recordOf(s Strategy, job modeladapter.VideoJob, candidate domain.CandidateStrategy) (json.RawMessage, error) {
	rec := adapterRecord{
		Candidate: candidate, Policy: s.Policy, Takes: s.Takes, Primary: s.Primary, Fallback: s.Fallback,
		CostPerTake: s.CostPerTake, Reasons: s.Reasons, Requests: map[domain.ModelName]recordedRequest{},
	}
	for _, t := range []modeladapter.VideoTarget{s.Primary, s.Fallback} {
		req, err := modeladapter.Prepare(t, job)
		if err != nil {
			return nil, err
		}
		rec.Requests[t.Model] = recordedRequest{
			Prompt: req.Prompt, NegativePrompt: req.NegativePrompt, DurationSeconds: req.DurationSeconds,
			Resolution: req.Resolution, AspectRatio: req.AspectRatio, ReferenceImages: len(req.References), FirstFrame: req.FirstFrame != nil,
		}
	}
	return json.Marshal(rec)
}

// takeParameters is the content of take.model_parameters.
type takeParameters struct {
	// TakeLineage records what the Take was generated from besides the Shot Spec (F-27).
	domain.TakeLineage
	Policy          domain.QualityPolicy `json:"quality_policy"`
	Resolution      string               `json:"resolution"`
	DurationSeconds float64              `json:"duration_seconds"`
	AspectRatio     string               `json:"aspect_ratio"`
	FellBack        bool                 `json:"fell_back,omitempty"`
	Prompt          string               `json:"prompt"`
	// Candidate is the strategy of the Prompt Candidate the Take was made for.
	Candidate domain.CandidateStrategy `json:"candidate_strategy,omitempty"`
}

// makeTake generates one Take, stores its file, the Take row and cost, and posts its card, whose
// title is title. The time spent on the model is bounded by limit (Max generation time of the
// Quality/Cost Policy). candidate is the Prompt Candidate strategy the Take is made for ("" for a
// plain generation or a repair).
func (o *Orchestrator) makeTake(ctx context.Context, ref repository.ShotRef, pv domain.PromptVersion, s Strategy, job modeladapter.VideoJob, lineage domain.TakeLineage, title string, limit time.Duration, candidate domain.CandidateStrategy) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	out, err := o.adapter.GenerateVideo(callCtx, s.Primary, s.Fallback, job)
	if err != nil {
		if ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%w (%s): %w", ErrGenerationTimeLimit, limit, err)
		}
		return "", err
	}
	asset, err := o.files.write(ref.ProjectID, domain.AssetVideo, out.MimeType, out.Data)
	if err != nil {
		return "", err
	}
	params, err := json.Marshal(takeParameters{
		Policy: s.Policy, Resolution: out.Resolution, DurationSeconds: out.DurationSeconds,
		AspectRatio: out.Request.AspectRatio, FellBack: out.FellBack, Prompt: out.Request.Prompt, Candidate: candidate, TakeLineage: lineage,
	})
	if err != nil {
		o.files.remove(asset)
		return "", err
	}
	takeID, err := o.gens.CreateTake(context.WithoutCancel(ctx), repository.NewTake{
		ProjectID: ref.ProjectID, ShotID: ref.Shot.ID, PromptVersionID: pv.ID, Model: out.Model,
		Parameters: params, Asset: asset, EstimatedCost: out.EstimatedCost, ActualCost: out.ActualCost,
	})
	if err != nil {
		o.files.remove(asset)
		return "", err
	}
	if candidate != "" {
		title += " · " + candidate.Label()
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindTake, RefID: takeID,
		Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
			Title:    title,
			Summary:  fmt.Sprintf("%s（%s・%.0f 秒）", out.Model, out.Resolution, out.DurationSeconds),
			VideoURL: domain.TakeVideoURL(takeID), PosterURL: domain.TakeThumbnailURL(takeID), ShotID: ref.Shot.ID,
		}),
	}
	if err := o.chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, card); err != nil {
		o.logger.Error("post take card failed", "take_id", takeID, "err", err)
	}
	return takeID, nil
}

// counter counts finished Takes from concurrent goroutines and remembers the first one.
type counter struct {
	mu      sync.Mutex
	n       int
	firstID string
}

func (c *counter) add(takeID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == 0 {
		c.firstID = takeID
	}
	c.n++
	return c.n
}

func (c *counter) value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// first is the ID of the Take that was finished first ("" when none).
func (c *counter) first() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firstID
}

// judgeTake has the AI Video Judge look at a Take that was just made. The Take is kept whether or
// not it can be judged: the user can run the Judge again.
func (o *Orchestrator) judgeTake(ctx context.Context, takeID string) {
	if o.judge == nil {
		return
	}
	if _, err := o.judge.Start(context.WithoutCancel(ctx), takeID); err != nil {
		o.logger.Error("start judge failed", "take_id", takeID, "err", err)
	}
}
