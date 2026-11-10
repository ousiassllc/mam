package genplan

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// Launches are the stages of the Shots that were claimed, by Shot ID.
type Launches = map[string]map[genapprovalrepo.Stage]genapprovalrepo.Launch

// Builder makes the Plan of a Project from what is stored.
type Builder struct {
	Plans    *repository.PlanRepository
	Gens     *repository.GenerationRepository
	Policies *repository.PolicyRepository
	// Config is the Model Adapter's current setting (prices, routes).
	Config func() modeladapter.Config
	// Candidates reports whether the generation of the Shot goes through Prompt Candidate Generation (generation.Orchestrator.UsesCandidates).
	Candidates func(ctx context.Context, shot domain.Shot) (bool, error)
}

// Build plans the Shots of the trunk that have no Take, are not being made and have not had their generation started: a Shot whose generation was
// started, or whose start was refused or failed, is not part of what is still to be started. launches are the stages that were claimed.
func (b Builder) Build(ctx context.Context, projectID string, launches Launches) (Plan, error) {
	return b.build(ctx, projectID, launches, "")
}

// BuildAs is Build as if the Project's Quality Policy were q: what the plan would be, and cost, if the user lowered the policy. It changes nothing.
func (b Builder) BuildAs(ctx context.Context, projectID string, launches Launches, q domain.QualityPolicy) (Plan, error) {
	return b.build(ctx, projectID, launches, q)
}

func (b Builder) build(ctx context.Context, projectID string, launches Launches, as domain.QualityPolicy) (Plan, error) {
	policy, err := b.Policies.Get(ctx, projectID)
	if err != nil {
		return Plan{}, err
	}
	if as != "" {
		policy.Quality = as
	}
	budget, err := b.Gens.Budget(ctx, projectID, "")
	if err != nil {
		return Plan{}, err
	}
	cfg := b.Config()
	pack, packModels, packUnpriced := packCost(cfg)
	plan := Plan{Exists: map[string]bool{}, packUnpriced: packUnpriced, ProjectCeiling: budget.ProjectCeiling, ShotCeiling: budget.ShotCeiling, ProjectSpent: budget.ProjectSpent}
	scenes, err := b.Plans.ListScenes(ctx, projectID)
	if err != nil {
		return Plan{}, err
	}
	number := 0
	for _, sc := range scenes {
		shots, err := b.Plans.ListShots(ctx, sc.ID)
		if err != nil {
			return Plan{}, err
		}
		takes, err := b.Plans.ListTakes(ctx, sc.ID)
		if err != nil {
			return Plan{}, err
		}
		for _, shot := range shots {
			number++
			plan.Exists[shot.ID] = true
			if !planned(shot, takes[shot.ID], launches[shot.ID]) {
				continue
			}
			line := Line{ShotID: shot.ID, Number: number, Seconds: shot.Duration}
			if _, started := launches[shot.ID][genapprovalrepo.StageReferencePack]; !started && !hasKeyframe(shot) {
				line.Pack, line.PackModels, line.PackCost = true, packModels, pack
				plan.PackImageModels = packModels
			}
			candidates, err := b.video(ctx, cfg, policy, shot, &line)
			if err != nil {
				return Plan{}, err
			}
			plan.Candidates = plan.Candidates || candidates
			plan.Lines = append(plan.Lines, line)
		}
	}
	plan.Unpriced = plan.unpriced()
	return plan, nil
}

// planned reports whether the Shot is still to be made: it is PLANNING with no Take and no stage of it was refused or failed, nor was its generation started.
func planned(shot domain.Shot, takes []domain.Take, launched map[genapprovalrepo.Stage]genapprovalrepo.Launch) bool {
	if shot.State != domain.ShotPlanning || len(takes) > 0 {
		return false
	}
	if _, made := launched[genapprovalrepo.StageGenerate]; made {
		return false
	}
	for _, l := range launched {
		if l.Outcome != genapprovalrepo.OutcomeStarted {
			return false
		}
	}
	return true
}

func hasKeyframe(shot domain.Shot) bool {
	return slices.ContainsFunc(shot.References(), func(r domain.Reference) bool { return r.Kind == domain.RefKeyframe })
}

// video fills the video side of the line the way a generation would decide it, without Multi-model routing (which looks at the Judge record at
// the time, and would make the plan change while it is carried out) and without a budget (the ceilings are applied when each start is made).
// It reports whether the Shot goes through Prompt Candidate Generation.
func (b Builder) video(ctx context.Context, cfg modeladapter.Config, policy domain.ProjectPolicy, shot domain.Shot, line *Line) (bool, error) {
	locks, err := b.Gens.ShotLocks(ctx, shot.ID)
	if err != nil {
		return false, err
	}
	strategy, err := generation.DecideWithModel(cfg, policy.Quality, shot.Duration, repository.Budget{}, generation.FixedModel(locks))
	if err != nil { // no route for the policy: the price of this Shot cannot be told
		line.VideoCost = Cost{Unknown: true}
		return false, nil
	}
	candidates, err := b.Candidates(ctx, shot)
	if err != nil {
		return false, err
	}
	line.VideoModel, line.Resolution, line.ClipSeconds, line.Takes = string(strategy.Primary.Model), strategy.Primary.Resolution, strategy.DurationSeconds, strategy.Takes
	if candidates {
		line.Takes = generation.PlannedTakes(domain.ModeOf(policy.Quality), policy.Effective(), strategy)
	}
	one := priced(cfg, strategy.Primary.Model, strategy.CostPerTake)
	line.VideoCost = Cost{USD: one.USD * float64(line.Takes), Unknown: one.Unknown}
	return candidates, nil
}

// UnpricedModels names the models the Project would use whose price is not known (#1139): the calls a budget ceiling cannot count. It is read from the plan of the Shots
// still to be made, and from the Quality Policy's routes when there is none yet (a Project that has only a Brief).
func (b Builder) UnpricedModels(ctx context.Context, projectID string, launches Launches) ([]string, error) {
	plan, err := b.Build(ctx, projectID, launches)
	if err != nil || len(plan.Lines) > 0 {
		return plan.Unpriced, err
	}
	policy, err := b.Policies.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return UnpricedRoutes(b.Config(), policy.Quality), nil
}
