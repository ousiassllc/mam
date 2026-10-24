package generation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/planning"
	"github.com/ousiassllc/mam/backend/internal/agent/promptplanner"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// errInvalidPlan is the error class of an AI answer that cannot be used.
var errInvalidPlan = planning.ErrInvalidPlan

var ratioRe = regexp.MustCompile(`(\d+)\s*[:：]\s*(\d+)`)

// aspectRatio reads the Brief's aspect ratio ("9:16", "縦 9:16"); an unsupported or missing one is the default.
func aspectRatio(brief domain.Brief) string {
	if m := ratioRe.FindStringSubmatch(brief.AspectRatio); m != nil && domain.IsBriefAspectRatio(m[1]+":"+m[2]) {
		return m[1] + ":" + m[2]
	}
	return domain.DefaultAspectRatio
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

// prepareGenerate is the Canonical Shot Spec a generate job will use, checked against LOCK brand
// constraints before the job is queued. A nil spec means the job should plan (or keep) it itself:
// the planner failed for a reason other than a brand violation, which the job reports.
// A brand violation is returned so nothing is queued.
func (o *Orchestrator) prepareGenerate(ctx context.Context, ref repository.ShotRef, instruction string, preserve []domain.SpecPart) (*domain.ShotSpec, []domain.NoteRef, error) {
	shot := ref.Shot
	current, hasSpec := shot.CanonicalSpec()
	locks, err := o.gens.ShotLocks(ctx, shot.ID)
	if err != nil {
		return nil, nil, err
	}
	if hasSpec && (instruction == "" || locks.Locked(domain.ParamPrompt)) {
		brand, err := o.brandConstraints(ctx, ref.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		if err := brand.CheckText(current.BrandText()); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}
	if err := refuseBusy(ref); err != nil {
		return nil, nil, err // before the Prompt Planner is paid for
	}
	sc, err := o.loadShotContext(ctx, ref, instruction)
	if err != nil {
		return nil, nil, err
	}
	in := sc.plannerInput(ref, instruction)
	if hasSpec {
		in.Current = &current
		in.Fixed = slices.Concat(locks.LockedParts(), preserve)
		in.Manual = locks.PartsWith(domain.LockManual)
	}
	spec, err := o.planner.Plan(ctx, in)
	if err != nil {
		if errors.Is(err, domain.ErrBrandViolation) {
			return nil, nil, err
		}
		return nil, nil, nil
	}
	return &spec, domain.NoteRefs(sc.notes), nil
}

// generate runs Prompt Autopilot and the Model Adapter for a Shot: it writes the Canonical Shot
// Spec, decides the strategy, and makes the Takes. planned, when set, is the spec prepareGenerate
// already checked; the planner is not called again.
func (o *Orchestrator) generate(ctx context.Context, ref repository.ShotRef, instruction string, preserve []domain.SpecPart, planned *domain.ShotSpec, plannedNotes []domain.NoteRef) (Progress, error) {
	shot := ref.Shot
	sc, err := o.loadShotContext(ctx, ref, instruction)
	if err != nil {
		return Progress{}, err
	}
	if err := sc.brand.CheckText(instruction); err != nil {
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
	switch {
	case planned != nil:
		spec, noteRefs = *planned, plannedNotes
	case !hasSpec || (instruction != "" && !locks.Locked(domain.ParamPrompt)):
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

	// The spec of the Shot may predate a constraint, and a LOCKed Prompt is never rewritten.
	if err := sc.brand.CheckText(spec.BrandText()); err != nil {
		return Progress{}, err
	}

	// Generation strategy: Shot Spec duration, cost and model capabilities.
	budget, err := o.gens.Budget(ctx, ref.ProjectID, shot.ID)
	if err != nil {
		return Progress{}, err
	}
	strategy, err := o.decide(ctx, ref, policy, &spec, budget)
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
	errs := parallel(o.logger, strategy.Takes, func(i int) error {
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
	brand domain.BrandConstraints
}

// loadShotContext reads the Policy, Brief, World State, neighbouring spec, Reference Pack, notes and
// Brand constraints of the Shot; instruction is the user's extra direction, which the choice of notes
// takes into account.
func (o *Orchestrator) loadShotContext(ctx context.Context, ref repository.ShotRef, instruction string) (shotContext, error) {
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return shotContext{}, err
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return shotContext{}, err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return shotContext{}, err
	}
	shots, err := o.gens.StoryShots(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return shotContext{}, err
	}
	notes, err := o.shotNotes(ctx, ref, instruction)
	if err != nil {
		return shotContext{}, err
	}
	brand, err := o.brandConstraints(ctx, ref.ProjectID)
	if err != nil {
		return shotContext{}, err
	}
	return shotContext{policy: policy, brief: brief, world: world, previous: previousSpec(shots, ref.Shot.ID), references: ref.Shot.References(), notes: notes, brand: brand}, nil
}

// brandConstraints are the Safety / Brand Constraints of the Project (none without a repository).
func (o *Orchestrator) brandConstraints(ctx context.Context, projectID string) (domain.BrandConstraints, error) {
	if o.brands == nil {
		return nil, nil
	}
	return o.brands.List(ctx, projectID)
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
		Instruction: instruction, World: c.world, Previous: c.previous, References: c.references, Notes: c.notes, Brand: c.brand,
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
