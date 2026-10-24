package repair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

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
	var latest *domain.JudgeResult
	switch {
	case err == nil:
		latest = &res
	case !errors.Is(err, repository.ErrNotFound):
		return domain.Job{}, err
	}
	policy, err := a.Policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return domain.Job{}, err
	}
	shot, err := a.Generations.GetShot(ctx, ref.Take.ShotID)
	if err != nil {
		return domain.Job{}, err
	}
	// The claim below decides who owns the Shot; this is the decision the Shot card draws its Repair button by.
	switch domain.CheckRepair(latest, policy.Effective(), policy.Template, shot.Shot.State) {
	case domain.RepairNotJudged:
		return domain.Job{}, ErrNotJudged
	case domain.RepairAccepted:
		return domain.Job{}, ErrAccepted
	case domain.RepairBusy:
		return domain.Job{}, ErrBusy
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
	if job.Kind == domain.JobGenerate && p.Action == generation.ActionReferencePack {
		return // a Reference Pack made no Take
	}
	a.Continue(ctx, p.ShotID)
}

// Continue is one look of the automatic loop at a Shot (see JobChanged). Every job that ends makes a
// look (a generate job, a judge job, the repair job itself), so looks overlap. A look reads the Shot
// and then claims it, and a repair of the Shot can begin and end in between: the look would then
// repair, or ask the user about, a Judge result that is no longer the latest. So only one look at a
// Shot runs at a time, and the one that waits reads the Shot afresh.
func (a *Agent) Continue(ctx context.Context, shotID string) {
	unlock, err := a.looks.Lock(ctx, shotID)
	if err != nil {
		return // ctx ended
	}
	defer unlock()
	shot, err := a.Generations.GetShot(ctx, shotID)
	if err != nil {
		return
	}
	project, err := a.Projects.Get(ctx, shot.ProjectID)
	if err != nil {
		a.Logger.Error("repair loop: read project failed", "project_id", shot.ProjectID, "err", err)
		return
	}
	if project.Mode != domain.ModeAuto || shot.Shot.State.Busy() {
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
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.TakeID == "" || p.ShotID == "" || (p.JudgeResultID == "" && p.Partial == nil) {
		return fmt.Errorf("repair job %s: invalid payload", job.ID)
	}
	// The Shot was claimed (REPAIRING) before this job was queued: give it back whichever way the
	// handler ends, a failed read of the Shot or a panic included. It is given back before done /
	// failed is announced (the client reads the Shot again on it and must not find it REPAIRING),
	// and exactly once.
	released := false
	release := func() {
		if !released {
			released = true
			a.release(ctx, p.ShotID)
		}
	}
	defer release()
	shot, err := a.Generations.GetShot(ctx, p.ShotID)
	if err != nil {
		return err
	}
	// The Shot may have been reset by Recover while this job waited in the queue.
	if err := a.Generations.SetShotState(ctx, p.ShotID, domain.ShotRepairing); err != nil {
		return err
	}
	a.Notify(generation.Progress{ShotID: p.ShotID, Action: generation.ActionRepair, Stage: generation.StageRepairing, Message: "修復戦略を選んでいます"})

	note := &doneNote{}
	madeTake, err := a.run(context.WithValue(ctx, doneNoteKey{}, note), shot, p)
	if err != nil {
		return a.fail(ctx, shot, err, release)
	}
	release()
	a.Notify(generation.Progress{ShotID: p.ShotID, Action: generation.ActionRepair, Stage: generation.StageDone, Message: note.message})
	if madeTake {
		// The new Take is judged by its own job, which may end before this one releases the Shot: look
		// at the Shot again once it is free, so that the loop goes on whichever ends last.
		a.Continue(context.WithoutCancel(ctx), p.ShotID)
	}
	return nil
}

// doneNote carries what run has to say in the done notification (an Escalation's reason) up to
// Handle, which announces it once the Shot is released.
type doneNote struct{ message string }

type doneNoteKey struct{}
