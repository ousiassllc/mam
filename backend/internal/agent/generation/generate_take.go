package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

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
	// Routing is why the Take's video model was chosen (Multi-model routing, F-27).
	Routing *domain.RoutingDecision `json:"routing,omitempty"`
}

// makeTake generates one Take, stores its file, the Take row and cost, and posts its card, whose
// title is title. The time spent on the model is bounded by limit (Max generation time of the
// Quality/Cost Policy). candidate is the Prompt Candidate strategy the Take is made for ("" for a
// plain generation or a repair).
func (o *Orchestrator) makeTake(ctx context.Context, ref repository.ShotRef, pv domain.PromptVersion, s Strategy, job modeladapter.VideoJob, lineage domain.TakeLineage, title string, limit time.Duration, candidate domain.CandidateStrategy) (string, error) {
	// The ledger gets the cost only when the video comes back, minutes from now. Until then the
	// estimate is held, so that Takes of other Shots, Repairs and Candidates that start at the same
	// time see it and cannot together spend more than the budget ceilings allow (F-22).
	release, ok, err := o.gens.Reserve(ctx, ref.ProjectID, ref.Shot.ID, s.CostPerTake)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrBudgetExceeded
	}
	defer release() // also when the generation fails or the cost was written by recordPaid below
	// The limit counts model time: the Adapter starts it when a vendor slot is held, not while the
	// Take waits for other generations to free one.
	job.MaxTime = limit
	out, err := o.adapter.GenerateVideo(ctx, s.Primary, s.Fallback, job)
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, modeladapter.ErrTimeLimit) {
			return "", fmt.Errorf("%w (%s): %w", ErrGenerationTimeLimit, limit, err)
		}
		return "", err
	}
	// The provider has charged for the video. Unless CreateTake writes it with the Take, the cost
	// goes to the ledger by itself, so a local failure never leaves the spend uncounted.
	taken := false
	defer func() {
		if !taken {
			o.recordPaid(ctx, ref.ProjectID, ref.Shot.ID, out.Model, out.EstimatedCost, out.ActualCost)
		}
	}()
	asset, err := o.files.store(ref.ProjectID, domain.AssetVideo, out.MimeType, out.File)
	if err != nil {
		return "", err
	}
	params, err := json.Marshal(takeParameters{
		Policy: s.Policy, Resolution: out.Resolution, DurationSeconds: out.DurationSeconds,
		AspectRatio: out.Request.AspectRatio, FellBack: out.FellBack, Prompt: out.Request.Prompt, Candidate: candidate, TakeLineage: lineage,
		Routing: s.routing(),
	})
	if err != nil {
		o.files.remove(asset)
		return "", err
	}
	takeID, err := o.gens.CreateReservedTake(context.WithoutCancel(ctx), repository.NewTake{
		ProjectID: ref.ProjectID, ShotID: ref.Shot.ID, PromptVersionID: pv.ID, Model: out.Model,
		Parameters: params, Asset: asset, EstimatedCost: out.EstimatedCost, ActualCost: out.ActualCost,
	}, release) // the hold goes as the cost reaches the ledger: a Reserve in between would count it twice
	if err != nil {
		o.files.remove(asset)
		return "", err
	}
	taken = true
	if candidate != "" {
		title += " · " + candidate.Label()
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindTake, RefID: takeID,
		Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
			Title:    title,
			Summary:  fmt.Sprintf("%s（%s・%.0f 秒）", out.Model, out.Resolution, out.DurationSeconds),
			VideoURL: domain.TakeVideoURL(takeID), PosterURL: domain.TakeThumbnailURL(takeID), ShotID: ref.Shot.ID, FPS: o.files.frameRate(context.WithoutCancel(ctx), asset),
		}),
	}
	if err := o.chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, card); err != nil {
		o.logger.Error("post take card failed", "take_id", takeID, "err", err)
	}
	return takeID, nil
}

// recordPaid writes the cost of an output the provider has charged for to the ledger when no Take
// or Pack row will carry it. It does not depend on ctx: a client that went away must not leave
// the spend uncounted (#243). A failed write is logged; the generation error stays the result.
func (o *Orchestrator) recordPaid(ctx context.Context, projectID, shotID string, model domain.ModelName, estimated float64, actual *float64) {
	err := o.gens.AddCost(context.WithoutCancel(ctx), domain.CostEntry{
		ProjectID: projectID, ShotID: shotID, Provider: model, EstimatedCost: estimated, ActualCost: actual,
	})
	if err != nil {
		o.logger.Error("record cost failed", "shot_id", shotID, "model", model, "err", err)
	}
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

// Progress vocabulary of the AI Video Judge, which reports on the same WebSocket (/ws/shots/:id).
const (
	StageJudging Stage  = "judging" // the AI Video Judge is watching a Take
	ActionJudge  Action = "judge"   // a job of the judge kind
)

// Judger runs the AI Video Judge on a Take. *judge.Runner implements it. The Orchestrator starts it
// for every Take it makes, so a Take is judged without a request from the user (spec §13), and
// evaluates Prompt Candidates in place because it needs their verdicts to pick the Best Take (§8).
type Judger interface {
	Start(ctx context.Context, takeID string) (domain.Job, error)
	// Evaluate judges the Take before it returns and returns the stored result.
	Evaluate(ctx context.Context, takeID string) (domain.JudgeResult, error)
}

// updateWorldState runs the State Extractor on a Shot whose Takes have been made and appends the
// resulting World State version (F-10, spec/spec.md §9.3). takeID is the Take the State Diff is
// recorded against. The Takes already exist and cost money, so a failure is logged and never
// fails the generation: the World State then simply stays as it was and the next Shot starts
// from it.
func (o *Orchestrator) updateWorldState(ctx context.Context, ref repository.ShotRef, spec domain.ShotSpec, takeID string) {
	world, err := o.world.World(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		o.logger.Warn("world state update skipped: read world state", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	locks, err := o.world.AgentLocks(ctx, ref.ProjectID)
	if err != nil {
		o.logger.Warn("world state update skipped: read locks", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	diff, err := o.extractor.Extract(ctx, stateextractor.Input{Scene: ref.Scene, Shot: ref.Shot, Spec: spec, World: world, Locks: locks})
	if err != nil {
		o.logger.Warn("world state update skipped: state extractor failed", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	// ApplyStateDiff judges the LOCKs (and MANUAL values) again on the rows it writes.
	applied, version, err := o.world.ApplyStateDiff(ctx, ref.ProjectID, ref.Scene.BranchID, ref.Shot.ID, takeID, diff)
	if err != nil {
		o.logger.Warn("world state update failed", "shot_id", ref.Shot.ID, "err", err)
		return
	}
	o.logger.Info("world state updated", "shot_id", ref.Shot.ID, "version", version, "changes", len(applied.Changes))
}
