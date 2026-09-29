package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
	Policy      domain.QualityPolicy                 `json:"quality_policy"`
	Takes       int                                  `json:"takes"`
	Primary     modeladapter.VideoTarget             `json:"primary"`
	Fallback    modeladapter.VideoTarget             `json:"fallback"`
	CostPerTake float64                              `json:"estimated_cost_per_take"`
	Reasons     []string                             `json:"reasons,omitempty"`
	Requests    map[domain.ModelName]recordedRequest `json:"requests"`
}

// generate runs Prompt Autopilot and the Model Adapter for a Shot: it writes the Canonical Shot
// Spec, decides the strategy, and makes the Takes.
func (o *Orchestrator) generate(ctx context.Context, ref repository.ShotRef, instruction string) (Progress, error) {
	shot := ref.Shot
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return Progress{}, err
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return Progress{}, err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID)
	if err != nil {
		return Progress{}, err
	}
	shots, err := o.gens.ListProjectShots(ctx, ref.ProjectID)
	if err != nil {
		return Progress{}, err
	}
	references := shot.References()

	// Prompt Autopilot (F-06). An existing spec is kept unless the user gave a new direction.
	current, hasSpec := shot.CanonicalSpec()
	spec := current
	if !hasSpec || instruction != "" {
		in := promptplanner.Input{
			Brief: brief, Template: policy.Template, Scene: ref.Scene, Shot: shot,
			Instruction: instruction, World: world, Previous: previousSpec(shots, shot.ID), References: references,
		}
		if hasSpec {
			in.Current = &current
		}
		if spec, err = o.planner.Plan(ctx, in); err != nil {
			return Progress{}, err
		}
	}

	// Generation strategy: Shot Spec duration, cost and model capabilities.
	budget, err := o.gens.Budget(ctx, ref.ProjectID, shot.ID)
	if err != nil {
		return Progress{}, err
	}
	strategy, err := Decide(o.adapter.Config(), policy.Quality, shot.Duration, budget)
	if err != nil {
		return Progress{}, err
	}
	job, err := o.videoJob(ctx, ref.ProjectID, spec, brief, strategy.DurationSeconds, references)
	if err != nil {
		return Progress{}, err
	}
	record, err := recordOf(strategy, job)
	if err != nil {
		return Progress{}, err
	}
	pv, err := o.gens.SavePromptedSpec(ctx, shot.ID, spec, record)
	if err != nil {
		return Progress{}, err
	}

	// Takes (F-12). They run at the same time; the Model Adapter limits calls per vendor.
	o.notify(Progress{ShotID: shot.ID, Action: ActionGenerate, Stage: StageGenerating, Message: "動画を生成しています", TakesTotal: strategy.Takes})
	var made counter
	errs := parallel(strategy.Takes, func(i int) error {
		takeID, err := o.makeTake(ctx, ref, pv, strategy, job, i, policy.Effective().MaxGenerationTime)
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
func recordOf(s Strategy, job modeladapter.VideoJob) (json.RawMessage, error) {
	rec := adapterRecord{
		Policy: s.Policy, Takes: s.Takes, Primary: s.Primary, Fallback: s.Fallback,
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
	Policy          domain.QualityPolicy `json:"quality_policy"`
	Resolution      string               `json:"resolution"`
	DurationSeconds float64              `json:"duration_seconds"`
	AspectRatio     string               `json:"aspect_ratio"`
	FellBack        bool                 `json:"fell_back,omitempty"`
	Prompt          string               `json:"prompt"`
}

// makeTake generates one Take, stores its file, the Take row and cost, and posts its card.
// The time spent on the model is bounded by limit (Max generation time of the Quality/Cost Policy).
func (o *Orchestrator) makeTake(ctx context.Context, ref repository.ShotRef, pv domain.PromptVersion, s Strategy, job modeladapter.VideoJob, index int, limit time.Duration) (string, error) {
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
		AspectRatio: out.Request.AspectRatio, FellBack: out.FellBack, Prompt: out.Request.Prompt,
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
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindTake, RefID: takeID,
		Content: domain.EncodeTakeCardBody(domain.TakeCardBody{
			Title:    fmt.Sprintf("Shot %d · Take %d", ref.Shot.OrderIndex+1, index+1),
			Summary:  fmt.Sprintf("%s（%s・%.0f 秒）", out.Model, out.Resolution, out.DurationSeconds),
			VideoURL: fmt.Sprintf("/projects/%s/assets/%s", ref.ProjectID, asset.ID),
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
