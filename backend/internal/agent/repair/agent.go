// Package repair is the Repair Agent and the Repair / Retake Loop (spec/spec.md §13, F-14): from the
// Judge result of a failed Take it chooses a repair strategy, makes the new Take within the Retry
// Policy, and asks the user when the policy stops the loop.
//
// The judgements that must be exact are deterministic rules and never go through a model: whether a
// Take is accepted (verdict, LOCK violations = critical constraints, minimum scores), whether the
// Retry Policy allows another try, and which strategies are possible at all. Only the choice among
// the remaining strategies is the Repair Agent's (the Reasoning LLM). TypeSafe AI Jev runs beside
// that choice in shadow mode and is measured, never obeyed (spec/architecture/model-adapters.md §4.5).
package repair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/jev"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

var (
	// ErrNotJudged means the Take has no Judge result to repair from (409).
	ErrNotJudged = errors.New("repair: the take has not been judged")
	// ErrAccepted means the Take passed the Judge and the Retry Policy's minimum scores: nothing to repair (409).
	ErrAccepted = errors.New("repair: the take is accepted")
	// ErrBusy means the Shot is being generated or repaired (409, no double execution).
	ErrBusy = generation.ErrShotBusy
)

// Chat is what the Repair Agent needs from the chat thread. *service.ChatService implements it.
type Chat interface {
	generation.Chat
	RaiseEscalation(ctx context.Context, projectID, text string, options []string, shotID string) error
}

// Regenerator makes the new Take of a repair. *generation.Orchestrator implements it.
type Regenerator interface {
	RegenerationStrategy(ctx context.Context, ref repository.ShotRef, r generation.Regeneration) (generation.Strategy, error)
	Regenerate(ctx context.Context, ref repository.ShotRef, r generation.Regeneration) (string, error)
}

// Shadow is the Jev client of the shadow run. *jev.Client implements it.
type Shadow interface {
	Choose(ctx context.Context, in jev.Input) (jev.Choice, error)
	Model() string
}

// Deps are the collaborators of an Agent.
type Deps struct {
	Generations *repository.GenerationRepository
	Judges      *repository.JudgeRepository
	Repairs     *repository.RepairRepository
	JobRepo     *repository.JobRepository
	Policies    *repository.PolicyRepository
	Projects    *repository.ProjectRepository
	Regenerator Regenerator
	Chat        Chat
	Jobs        generation.Jobs
	// LLM is the Reasoning LLM Adapter the Repair Agent reasons through.
	LLM llm.Completer
	// Shadow, when set, is asked the same question as the Repair Agent whenever the Repair Agent
	// chooses. Its answer is only recorded.
	Shadow Shadow
	// Notify receives every progress notification. It MUST NOT block.
	Notify func(generation.Progress)
	Logger *slog.Logger
}

// Agent is the Repair Agent. It is safe for concurrent use.
type Agent struct {
	Deps
}

// New returns an Agent.
func New(d Deps) *Agent {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Notify == nil {
		d.Notify = func(generation.Progress) {}
	}
	return &Agent{d}
}

// payload is the payload of a repair job.
type payload struct {
	TakeID        string `json:"take_id"`
	ShotID        string `json:"shot_id"`
	JudgeResultID string `json:"judge_result_id"`
	// Auto is true when the loop started the job by itself after a Judge run (AUTO mode).
	Auto bool `json:"auto,omitempty"`
}

// Start queues a repair of the Take from its latest Judge result. It returns ErrNotJudged when
// there is none, ErrAccepted when the Take is accepted, ErrBusy when the Shot is being generated
// or repaired, and repository.ErrNotFound for an unknown Take. The Retry Policy is applied by the job:
// at its limit the job asks the user (an Escalation card) instead of making a Take.
func (a *Agent) Start(ctx context.Context, takeID string, auto bool) (domain.Job, error) {
	ref, err := a.Judges.GetTake(ctx, takeID)
	if err != nil {
		return domain.Job{}, err
	}
	res, err := a.Judges.Latest(ctx, takeID)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.Job{}, ErrNotJudged
	}
	if err != nil {
		return domain.Job{}, err
	}
	policy, err := a.Policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return domain.Job{}, err
	}
	if res.Accepted(policy.Effective(), policy.Template) {
		return domain.Job{}, ErrAccepted
	}
	shot, err := a.Generations.GetShot(ctx, ref.Take.ShotID)
	if err != nil {
		return domain.Job{}, err
	}
	claimed, err := a.Generations.StartRepair(ctx, shot.Shot.ID)
	if err != nil {
		return domain.Job{}, err
	}
	if !claimed {
		return domain.Job{}, ErrBusy
	}
	job, err := a.Jobs.Enqueue(ctx, domain.JobRepair, payload{TakeID: takeID, ShotID: shot.Shot.ID, JudgeResultID: res.ID, Auto: auto})
	if err != nil {
		a.release(ctx, shot.Shot.ID)
		return domain.Job{}, fmt.Errorf("enqueue repair job: %w", err)
	}
	card := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindProgress, RefID: job.ID,
		Content: fmt.Sprintf("Shot %d を Repair しています（Judge 結果から戦略を選んで再生成）", shot.Shot.OrderIndex+1),
	}
	if err := a.Chat.Announce(ctx, shot.ProjectID, card); err != nil {
		a.Logger.Error("post repair progress card failed", "shot_id", shot.Shot.ID, "job_id", job.ID, "err", err)
	}
	a.Notify(generation.Progress{ShotID: shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageQueued})
	return job, nil
}

// release gives the Shot back (no repair is running). It works on a context that survives shutdown.
func (a *Agent) release(ctx context.Context, shotID string) {
	if err := a.Generations.SetShotState(context.WithoutCancel(ctx), shotID, domain.ShotPlanning); err != nil && !errors.Is(err, repository.ErrNotFound) {
		a.Logger.Error("release shot failed", "shot_id", shotID, "err", err)
	}
}

// JobChanged continues the automatic loop when a generate or judge job of a Shot succeeded: in AUTO
// mode, once every Take of the Shot has been judged and none is accepted, the best of them is
// repaired. It runs on the goroutine of the caller, so call it in one (the job queue's callback must
// not block). DIRECTOR MODE never repairs by itself: the user asks (POST /takes/:id/repair).
//
// The loop is driven by job completion, not by the Judge, so that it sees the Shot after the job
// released it: a Take judged while its Shot is still GENERATING is picked up when generation ends.
func (a *Agent) JobChanged(ctx context.Context, jobID string) {
	job, err := a.JobRepo.Get(ctx, jobID)
	if err != nil {
		return
	}
	if job.Status != domain.JobSucceeded || (job.Kind != domain.JobGenerate && job.Kind != domain.JobJudge) {
		return
	}
	var p struct {
		Action generation.Action `json:"action"`
		ShotID string            `json:"shot_id"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ShotID == "" {
		return
	}
	if job.Kind == domain.JobGenerate && p.Action != generation.ActionGenerate {
		return // a Reference Pack made no Take
	}
	a.Continue(ctx, p.ShotID)
}

// Continue is one look of the automatic loop at a Shot (see JobChanged).
func (a *Agent) Continue(ctx context.Context, shotID string) {
	shot, err := a.Generations.GetShot(ctx, shotID)
	if err != nil {
		return
	}
	project, err := a.Projects.Get(ctx, shot.ProjectID)
	if err != nil {
		a.Logger.Error("repair loop: read project failed", "project_id", shot.ProjectID, "err", err)
		return
	}
	if project.Mode != domain.ModeAuto || shot.Shot.State == domain.ShotGenerating || shot.Shot.State == domain.ShotRepairing {
		return
	}
	policy, err := a.Policies.Get(ctx, shot.ProjectID)
	if err != nil {
		a.Logger.Error("repair loop: read policy failed", "project_id", shot.ProjectID, "err", err)
		return
	}
	verdicts, err := a.Repairs.ShotVerdicts(ctx, shotID)
	if err != nil {
		a.Logger.Error("repair loop: read takes failed", "shot_id", shotID, "err", err)
		return
	}
	base, ok := baseTake(verdicts, policy)
	if !ok {
		return
	}
	if _, err := a.Start(ctx, base, true); err != nil && !errors.Is(err, ErrBusy) && !errors.Is(err, ErrAccepted) {
		a.Logger.Error("repair loop: start failed", "take_id", base, "err", err)
	}
}

// baseTake is the Take an automatic repair starts from: none while a Take of the Shot is unjudged
// or being judged, or when one of them is accepted; otherwise the best judged Take (the newer one
// among equals).
func baseTake(verdicts []repository.TakeVerdict, policy domain.ProjectPolicy) (string, bool) {
	eff := policy.Effective()
	var (
		best   Scored
		baseID string
	)
	for _, v := range verdicts {
		if v.Result == nil || v.Pending || v.Result.Accepted(eff, policy.Template) {
			return "", false
		}
		if rank := Rank(*v.Result, policy.Template); baseID == "" || !best.Beats(rank) {
			best, baseID = rank, v.Take.ID
		}
	}
	return baseID, baseID != ""
}

// Handle is the job handler of domain.JobRepair. Register it as NOT resumable: a repair costs money,
// so an interrupted one is failed and the loop starts again from the next Judge result.
func (a *Agent) Handle(ctx context.Context, job domain.Job) error {
	var p payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.TakeID == "" || p.ShotID == "" || p.JudgeResultID == "" {
		return fmt.Errorf("repair job %s: invalid payload", job.ID)
	}
	shot, err := a.Generations.GetShot(ctx, p.ShotID)
	if err != nil {
		return err
	}
	defer a.release(ctx, p.ShotID)
	// The Shot may have been reset by Recover while this job waited in the queue.
	if err := a.Generations.SetShotState(ctx, p.ShotID, domain.ShotRepairing); err != nil {
		return err
	}
	a.Notify(generation.Progress{ShotID: p.ShotID, Action: generation.ActionRepair, Stage: generation.StageRepairing, Message: "修復戦略を選んでいます"})

	if err := a.run(ctx, shot, p); err != nil {
		return a.fail(ctx, shot, err)
	}
	a.Notify(generation.Progress{ShotID: p.ShotID, Action: generation.ActionRepair, Stage: generation.StageDone})
	return nil
}

// run is one step of the loop: the Retry Policy, the strategy, the new Take.
func (a *Agent) run(ctx context.Context, shot repository.ShotRef, p payload) error {
	take, err := a.Judges.GetTake(ctx, p.TakeID)
	if err != nil {
		return err
	}
	res, err := a.Judges.Get(ctx, p.JudgeResultID)
	if err != nil {
		return err
	}
	policy, err := a.Policies.Get(ctx, shot.ProjectID)
	if err != nil {
		return err
	}
	eff := policy.Effective()
	source := domain.ModelName(take.Take.ModelName)
	regen := generation.Regeneration{Action: domain.RepairFullRetake, SourceModel: source}

	history, err := a.Repairs.ShotActions(ctx, shot.Shot.ID)
	if err != nil {
		return err
	}
	retries := domain.RetriesDone(history)

	// The Retry Policy (deterministic): Max retries per Shot, Max cost per Shot, the Project budget.
	if ok, err := a.withinPolicy(ctx, shot, eff, retries, regen, res, take.Take.ID); err != nil || !ok {
		return err
	}

	locks, err := a.Generations.ShotLocks(ctx, shot.Shot.ID)
	if err != nil {
		return err
	}
	_, otherErr := a.Regenerator.RegenerationStrategy(ctx, shot, generation.Regeneration{Action: domain.RepairDifferentModel, SourceModel: source})
	situation := Situation{
		Result: res, Tried: domain.TriedActions(history), HasReferences: len(shot.Shot.References()) > 0,
		PromptLocked: locks.Locked(domain.ParamPrompt), OtherModel: otherErr == nil,
	}
	candidates := Candidates(situation)
	action, reason, decider, extra := a.decide(ctx, situation, candidates)

	regen.Action = action
	regen.Issues, regen.Preserve = res.Issues, res.Preserve
	regen.Instructions = mergeInstructions(res, extra)
	if action.Regenerates() && action != domain.RepairFullRetake {
		// The cost of the chosen strategy can differ (another model): apply the Retry Policy to it.
		if ok, err := a.withinPolicy(ctx, shot, eff, retries, regen, res, take.Take.ID); err != nil || !ok {
			return err
		}
	}

	rec, err := a.Repairs.CreateAction(ctx, res.ID, action, domain.RepairInstructions{
		Instructions: regen.Instructions, Reason: reason, DecidedBy: decider, SourceTakeID: take.Take.ID,
	})
	if err != nil {
		return err
	}

	var shadowDone sync.WaitGroup
	if decider == domain.DecidedByLLM && a.Shadow != nil {
		shadowDone.Add(1)
		go func() {
			defer shadowDone.Done()
			a.runShadow(context.WithoutCancel(ctx), rec.ID, res, candidates, action)
		}()
	}
	defer shadowDone.Wait()

	if action == domain.RepairEditLevel {
		return a.announceEditLevel(ctx, shot, take.Take.ID, res)
	}
	a.Notify(generation.Progress{ShotID: shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageRepairing, Message: action.Label() + " で新しい Take を作成しています"})
	newTake, err := a.Regenerator.Regenerate(ctx, shot, regen)
	if err != nil {
		return err
	}
	if err := a.Repairs.SetResultingTake(context.WithoutCancel(ctx), rec.ID, newTake); err != nil {
		return err
	}
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Shot %d を %s で再生成しました（再試行 %d/%d 回目）。新しい Take を Judge します。",
			shot.Shot.OrderIndex+1, action.Label(), retries+1, eff.MaxRetries),
	}
	if err := a.Chat.Announce(context.WithoutCancel(ctx), shot.ProjectID, notice); err != nil {
		a.Logger.Error("post repair notice failed", "shot_id", shot.Shot.ID, "err", err)
	}
	return nil
}

// withinPolicy applies the Retry Policy to the next regeneration of regen.Action. It returns false
// (after asking the user) when the policy stops the loop.
func (a *Agent) withinPolicy(ctx context.Context, shot repository.ShotRef, eff domain.EffectivePolicy, retries int, regen generation.Regeneration, res domain.JudgeResult, takeID string) (bool, error) {
	verdict, err := a.verdict(ctx, shot, eff, retries, regen)
	if err != nil {
		return false, err
	}
	if verdict == domain.RetryAllowed {
		return true, nil
	}
	return false, a.escalate(ctx, shot, takeID, res, verdict)
}

// verdict is the Retry Policy's answer for regen. A retry that no budget covers is a budget verdict.
func (a *Agent) verdict(ctx context.Context, shot repository.ShotRef, eff domain.EffectivePolicy, retries int, regen generation.Regeneration) (domain.RetryVerdict, error) {
	if retries >= eff.MaxRetries {
		return domain.RetryMaxRetries, nil // decided before any cost is estimated
	}
	strategy, err := a.Regenerator.RegenerationStrategy(ctx, shot, regen)
	if errors.Is(err, generation.ErrBudgetExceeded) {
		return budgetVerdict(eff), nil
	}
	if err != nil {
		return "", err
	}
	budget, err := a.Generations.Budget(ctx, shot.ProjectID, shot.Shot.ID)
	if err != nil {
		return "", err
	}
	return eff.CanRetry(retries, budget.ShotSpent, budget.ProjectSpent, strategy.CostPerTake), nil
}

// budgetVerdict names the ceiling that ends the loop when not even one Take fits the budget.
func budgetVerdict(eff domain.EffectivePolicy) domain.RetryVerdict {
	if eff.ShotBudgetCeiling != nil {
		return domain.RetryShotBudget
	}
	return domain.RetryProjectBudget
}

// decide chooses the strategy: the deterministic rules first, then the Repair Agent (LLM) among the
// candidates, then the fallback when it cannot answer. It returns the strategy, why, who decided
// and the extra instructions of the Repair Agent.
func (a *Agent) decide(ctx context.Context, s Situation, candidates []domain.RepairAction) (domain.RepairAction, string, domain.RepairDecider, []string) {
	if action, reason, ok := Rule(s, candidates); ok {
		return action, reason, domain.DecidedByRule, nil
	}
	if a.LLM != nil {
		c, err := choose(ctx, a.LLM, s, candidates)
		if err == nil {
			return c.Action, c.Reason, domain.DecidedByLLM, c.Instructions
		}
		if ctx.Err() == nil {
			a.Logger.Warn("repair agent could not choose; using the fallback", "err", err)
		}
	}
	action, reason := Fallback(s, candidates)
	return action, reason, domain.DecidedByRule, nil
}

// mergeInstructions joins the Judge's instructions with the Repair Agent's, without repeats.
func mergeInstructions(res domain.JudgeResult, extra []string) []string {
	var list []string
	if rs := res.RepairStrategy; rs != nil {
		list = append(list, rs.Instructions...)
	}
	return identifiers(append(list, extra...))
}

// runShadow asks Jev the question the Repair Agent just answered and records the outcome. Nothing
// it does or fails to do reaches the repair. Only structured Issue fields are sent.
func (a *Agent) runShadow(ctx context.Context, actionID string, res domain.JudgeResult, candidates []domain.RepairAction, decided domain.RepairAction) {
	out, err := a.Shadow.Choose(ctx, jev.Input{Issues: res.Issues, Candidates: candidates})
	if errors.Is(err, jev.ErrNoAPIKey) {
		return // Jev is not set up: nothing to measure
	}
	rec := domain.RepairShadow{RepairActionID: actionID, Model: a.Shadow.Model(), DecidedAction: decided}
	if err != nil {
		rec.Error = err.Error()
	} else {
		rec.Model, rec.JevChoice, rec.Probabilities = firstNonEmpty(out.Model, rec.Model), string(out.Action), out.Probabilities
		rec.Confidence = &out.Confidence
		rec.Agreed = out.Action == decided
		rec.LatencyMillis, rec.InputTokens, rec.OutputTokens, rec.CostUSD = out.Latency.Milliseconds(), out.InputTokens, out.OutputTokens, out.CostUSD
	}
	if rec.Error != "" {
		a.Logger.Warn("jev shadow run failed", "err", rec.Error)
	}
	if err := a.Repairs.SaveShadow(ctx, rec); err != nil {
		a.Logger.Error("save jev shadow run failed", "err", err)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// escalate asks the user for a decision when the Retry Policy stops the loop: an Escalation card,
// posted at every Confirmation Level.
func (a *Agent) escalate(ctx context.Context, shot repository.ShotRef, takeID string, res domain.JudgeResult, verdict domain.RetryVerdict) error {
	text := fmt.Sprintf("Shot %d の Repair を止めました: %s。\n最新の Judge 結果（Take %s）: %s",
		shot.Shot.OrderIndex+1, verdict.Reason(), shortID(takeID), Summarize(res))
	options := []string{
		"設定で上限を引き上げてから、もう一度 Repair を実行する",
		"最も良い Take をそのまま採用する",
		"Shot の指示を手動で修正して生成し直す",
	}
	a.Notify(generation.Progress{ShotID: shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageDone, Message: verdict.Reason()})
	return a.Chat.RaiseEscalation(context.WithoutCancel(ctx), shot.ProjectID, text, options, shot.Shot.ID)
}

// announceEditLevel tells the user which ranges of the Take to fix in the edit. Edit-Level Repair
// makes no new Take, so there is nothing to judge again.
func (a *Agent) announceEditLevel(ctx context.Context, shot repository.ShotRef, takeID string, res domain.JudgeResult) error {
	var ranges []string
	for _, is := range res.Issues {
		ranges = append(ranges, fmt.Sprintf("%s（%s）", is.TimeRange, is.Type))
	}
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Shot %d の Take %s は再生成せず、編集で直します（Edit-Level Repair）。該当区間: %s",
			shot.Shot.OrderIndex+1, shortID(takeID), strings.Join(ranges, "、")),
	}
	return a.Chat.Announce(context.WithoutCancel(ctx), shot.ProjectID, notice)
}

// fail tells the user why the repair stopped and returns the error for the job record.
func (a *Agent) fail(ctx context.Context, shot repository.ShotRef, err error) error {
	if ctx.Err() != nil {
		return err // shutting down: nobody is told
	}
	if errors.Is(err, repository.ErrNotFound) {
		return err
	}
	a.Logger.Error("repair failed", "shot_id", shot.Shot.ID, "err", err)
	reason := FailureReason(err)
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Shot %d の Repair に失敗しました: %s\n対処したら、もう一度 Repair を実行してください。", shot.Shot.OrderIndex+1, reason),
	}
	if aerr := a.Chat.Announce(context.WithoutCancel(ctx), shot.ProjectID, notice); aerr != nil {
		a.Logger.Error("post repair failure notice failed", "shot_id", shot.Shot.ID, "err", aerr)
	}
	a.Notify(generation.Progress{ShotID: shot.Shot.ID, Action: generation.ActionRepair, Stage: generation.StageFailed, Message: reason})
	return errors.New(reason)
}

// FailureReason is the user-facing cause of a failed repair. Details stay in the log.
func FailureReason(err error) string {
	switch {
	case errors.Is(err, generation.ErrNoSpec):
		return "この Shot にはまだ Shot Spec がありません（先に生成してください）"
	case errors.Is(err, generation.ErrNoOtherModel):
		return "切り替えられる別の動画モデルがありません"
	case errors.Is(err, repository.ErrLocked):
		return "LOCK されている項目のため、Shot Spec を書き換えられません"
	}
	return generation.FailureReason(err)
}

// Summarize is a one-line account of a Judge result for the chat: the verdict and its Issues.
func Summarize(res domain.JudgeResult) string {
	verdict := "FAIL"
	if res.Result == domain.JudgePass {
		verdict = "PASS（最低スコア未達）"
	}
	var parts []string
	for _, is := range res.Issues {
		p := fmt.Sprintf("%s[%s]", is.Type, is.Severity)
		if is.LockViolation {
			p += "（LOCK 違反）"
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return verdict
	}
	return verdict + " — " + strings.Join(parts, ", ")
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
