package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrBusy means the Take is already being judged (409, no double execution).
var ErrBusy = errors.New("judge: the take is already being judged")

// Deps are the collaborators of a Runner.
type Deps struct {
	Generations *repository.GenerationRepository
	Judges      *repository.JudgeRepository
	Assets      *repository.AssetRepository
	// Policies gives the Production Template of the Project, whose axes the Judge scores. Optional.
	Policies *repository.PolicyRepository
	// Brands gives the Safety / Brand Constraints of the Project, which the Judge enforces (F-23). Optional.
	Brands *repository.BrandRepository
	// Takes and Projects make the AI Pick after every result (F-15). Both nil: no Take is picked.
	Takes    *repository.TakeRepository
	Projects *repository.ProjectRepository
	Chat     generation.Chat
	Jobs     generation.Jobs
	Judge    *Judge
	// AssetsDir is the root of stored files (config.AssetsDir).
	AssetsDir string
	// Notify receives every progress notification. It MUST NOT block.
	Notify func(generation.Progress)
	// TakeJudged is called with the Take ID after a result is saved and announced, so that the Take
	// card already in the thread can redraw its Judge Issue markers. Optional.
	TakeJudged func(ctx context.Context, takeID string)
	Logger     *slog.Logger
}

// Runner runs the AI Video Judge as background jobs: it gathers what the Judge needs about a Take,
// runs it, stores the result and tells the user. It is safe for concurrent use.
type Runner struct {
	Deps

	mu      sync.Mutex
	running map[string]int // Shot ID -> judge jobs whose handler is executing

	startMu  sync.Mutex
	starting map[string]struct{} // Take IDs whose Start is between its pending check and its enqueue
}

// beginStart claims the Take for one Start call. The pending check and the enqueue of Start are two
// separate transactions, so without the claim two concurrent calls could both pass the check and
// queue a Judge job each. It reports false while another Start holds the Take.
func (r *Runner) beginStart(takeID string) bool {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	if _, busy := r.starting[takeID]; busy {
		return false
	}
	if r.starting == nil {
		r.starting = make(map[string]struct{})
	}
	r.starting[takeID] = struct{}{}
	return true
}

func (r *Runner) endStart(takeID string) {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	delete(r.starting, takeID)
}

func (r *Runner) enter(shotID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == nil {
		r.running = make(map[string]int)
	}
	r.running[shotID]++
}

// leave releases the Shot when the last judge job that runs on it ends. The count lives here
// rather than in the job table because a job is still marked running for a moment after its
// handler returned, so two jobs that end together would each see the other as running.
func (r *Runner) leave(ctx context.Context, shotID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[shotID]--; r.running[shotID] > 0 {
		return
	}
	delete(r.running, shotID)
	if err := r.Judges.EndJudging(ctx, shotID); err != nil {
		r.Logger.Error("release shot after judging failed", "shot_id", shotID, "err", err)
	}
}

// NewRunner returns a Runner.
func NewRunner(d Deps) *Runner {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Notify == nil {
		d.Notify = func(generation.Progress) {}
	}
	return &Runner{Deps: d}
}

type payload struct {
	TakeID string `json:"take_id"`
	ShotID string `json:"shot_id"`
}

// Start queues a Judge run on the Take (ErrBusy when one is already queued or running or being
// queued by a concurrent Start, repository.ErrNotFound for an unknown Take). Every run adds a
// result; earlier ones are kept.
func (r *Runner) Start(ctx context.Context, takeID string) (domain.Job, error) {
	ref, err := r.Judges.GetTake(ctx, takeID)
	if err != nil {
		return domain.Job{}, err
	}
	if !r.beginStart(takeID) {
		return domain.Job{}, ErrBusy
	}
	defer r.endStart(takeID)
	pending, err := r.Judges.JudgePending(ctx, takeID)
	if err != nil {
		return domain.Job{}, err
	}
	if pending {
		return domain.Job{}, ErrBusy
	}
	job, err := r.Jobs.Enqueue(ctx, domain.JobJudge, payload{TakeID: takeID, ShotID: ref.Take.ShotID})
	if err != nil {
		return domain.Job{}, fmt.Errorf("enqueue judge job: %w", err)
	}
	if err := r.Judges.BeginJudging(ctx, ref.Take.ShotID); err != nil {
		r.Logger.Error("mark shot judging failed", "shot_id", ref.Take.ShotID, "err", err)
	}
	r.Notify(generation.Progress{ShotID: ref.Take.ShotID, Action: generation.ActionJudge, Stage: generation.StageQueued})
	return job, nil
}

// Handle is the job handler of domain.JobJudge. Register it as resumable: judging costs little and
// is repeatable, so a run interrupted by an abnormal termination is simply run again.
func (r *Runner) Handle(ctx context.Context, job domain.Job) error {
	var p payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.TakeID == "" {
		return fmt.Errorf("judge job %s: invalid payload", job.ID)
	}
	ref, err := r.Judges.GetTake(ctx, p.TakeID)
	if err != nil {
		return err // the Take is gone
	}
	shotID := ref.Take.ShotID
	r.enter(shotID)
	// The Shot is released before done / failed is announced (the client reads the Shot again on
	// it and must not find it JUDGING), and exactly once however the handler ends.
	left := false
	leave := func() {
		if !left {
			left = true
			r.leave(context.WithoutCancel(ctx), shotID)
		}
	}
	defer leave()
	// From here on every error goes through fail, so the client gets StageFailed and the thread a
	// notice whichever step broke (the Judge, the database).
	if err := r.Judges.BeginJudging(ctx, shotID); err != nil {
		return r.fail(ctx, ref, err, leave)
	}
	r.Notify(generation.Progress{ShotID: shotID, Action: generation.ActionJudge, Stage: generation.StageJudging, Message: "Take を評価しています"})

	res, err := r.judge(ctx, ref)
	if err != nil {
		return r.fail(ctx, ref, err, leave)
	}
	if _, err := r.store(ctx, ref, res); err != nil {
		return r.fail(ctx, ref, err, leave)
	}
	leave()
	r.Notify(generation.Progress{ShotID: shotID, Action: generation.ActionJudge, Stage: generation.StageDone})
	return nil
}

// Evaluate judges the Take now, in the calling goroutine, and returns the stored result. It is
// for callers that need the verdict before they go on (the Best Take selection of Prompt Candidate
// Generation); it does not use the job queue and does not change the state of the Shot. The result
// is stored and posted to the thread like the one of a judge job. It returns
// repository.ErrNotFound for an unknown Take.
func (r *Runner) Evaluate(ctx context.Context, takeID string) (domain.JudgeResult, error) {
	ref, err := r.Judges.GetTake(ctx, takeID)
	if err != nil {
		return domain.JudgeResult{}, err
	}
	res, err := r.judge(ctx, ref)
	if err != nil {
		return domain.JudgeResult{}, err
	}
	return r.store(ctx, ref, res)
}

// store saves a Judge result and posts its summary to the thread.
func (r *Runner) store(ctx context.Context, ref repository.TakeRef, res domain.JudgeResult) (domain.JudgeResult, error) {
	saved, err := r.Judges.Save(context.WithoutCancel(ctx), res)
	if err != nil {
		return domain.JudgeResult{}, err
	}
	shot, err := r.Generations.GetShot(ctx, ref.Take.ShotID)
	if err != nil {
		return domain.JudgeResult{}, err
	}
	content := Summary(shot.Shot.OrderIndex+1, ref.Take.ID, saved) + r.pick(ctx, ref)
	notice := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
	if err := r.Chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, notice); err != nil {
		r.Logger.Error("post judge result failed", "take_id", ref.Take.ID, "err", err)
	}
	if r.TakeJudged != nil {
		r.TakeJudged(context.WithoutCancel(ctx), ref.Take.ID)
	}
	return saved, nil
}

// judge gathers the input of the Take and runs the Judge.
func (r *Runner) judge(ctx context.Context, ref repository.TakeRef) (domain.JudgeResult, error) {
	in, err := r.input(ctx, ref)
	if err != nil {
		return domain.JudgeResult{}, err
	}
	res, err := r.Judge.Evaluate(ctx, in)
	if err != nil {
		return domain.JudgeResult{}, err
	}
	res.TakeID = ref.Take.ID
	return res, nil
}

// fail tells the user why judging stopped and returns the error for the job record. The Shot is
// released (leave) before the failure is announced: the client reads the Shot again on it.
func (r *Runner) fail(ctx context.Context, ref repository.TakeRef, err error, leave func()) error {
	shotID := ref.Take.ShotID
	if ctx.Err() != nil {
		return err // shutting down: nobody is told
	}
	if errors.Is(err, repository.ErrNotFound) {
		return err
	}
	leave()
	r.Logger.Error("judge failed", "take_id", ref.Take.ID, "err", err)
	reason := FailureReason(err)
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Take %s の Judge に失敗しました: %s\n対処したら、もう一度 Judge を実行してください。", domain.ShortID(ref.Take.ID), reason),
	}
	if aerr := r.Chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, notice); aerr != nil {
		r.Logger.Error("post judge failure notice failed", "take_id", ref.Take.ID, "err", aerr)
	}
	r.Notify(generation.Progress{ShotID: shotID, Action: generation.ActionJudge, Stage: generation.StageFailed, Message: reason})
	return errors.New(reason)
}
