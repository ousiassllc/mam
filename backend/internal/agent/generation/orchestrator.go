package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/artdirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/promptplanner"
	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrShotBusy means the Shot is already being generated (409, no double execution).
var ErrShotBusy = errors.New("generation: the shot is already being generated")

// ErrGenerationTimeLimit means a Take took longer than the Max generation time of the Quality/Cost Policy.
var ErrGenerationTimeLimit = errors.New("generation: the generation time limit was exceeded")

// Stage is what a Shot is doing, as shown by the progress notifications (WebSocket /ws/shots/:id).
type Stage string

// Stages. StageJudging (judge.go) is added by the AI Video Judge.
const (
	StageIdle       Stage = "idle"       // nothing is running (first message of a connection)
	StageQueued     Stage = "queued"     // waiting in the job queue
	StageGenerating Stage = "generating" // writing the spec, making images or Takes
	StageDone       Stage = "done"
	StageFailed     Stage = "failed"
)

// Action is what a job of the generate kind does.
type Action string

// Job actions.
const (
	ActionGenerate      Action = "generate"       // Prompt Autopilot → Model Adapter → Takes
	ActionCandidates    Action = "candidates"     // Prompt Candidate Generation → Takes → Judge → Best Take
	ActionReferencePack Action = "reference_pack" // Art Director → image models → Reference Pack
)

// Progress is one notification about a Shot.
type Progress struct {
	ShotID     string `json:"shot_id"`
	Action     Action `json:"action,omitempty"`
	Stage      Stage  `json:"stage"`
	Message    string `json:"message,omitempty"`
	TakesDone  int    `json:"takes_done,omitempty"`
	TakesTotal int    `json:"takes_total,omitempty"`
}

// Chat is what the Generation Orchestrator needs from the chat thread. *service.ChatService implements it.
type Chat interface {
	Announce(ctx context.Context, projectID string, m domain.ChatMessage) error
}

// Jobs is the background job queue. *jobqueue.Queue implements it.
type Jobs interface {
	Enqueue(ctx context.Context, kind domain.JobKind, payload any) (domain.Job, error)
}

// Deps are the collaborators of an Orchestrator.
type Deps struct {
	Generations *repository.GenerationRepository
	Projects    *repository.ProjectRepository
	Policies    *repository.PolicyRepository
	Assets      *repository.AssetRepository
	// Repairs hold the Repair history of a Shot: Prompt Candidate Generation counts it against the
	// Max retries per Shot together with its own Takes (domain.RetriesDone, domain.CandidateRetries).
	Repairs *repository.RepairRepository
	// Brands gives the Safety / Brand Constraints the Prompt Planner follows (F-23). Optional.
	Brands *repository.BrandRepository
	// World holds the World State the State Extractor updates after each Shot.
	World *repository.WorldRepository
	// Notes are the production notes of the Project; the Prompt Planner is given the relevant ones.
	// Nil: no notes.
	Notes *repository.NoteRepository
	Chat  Chat
	Jobs  Jobs
	// LLM is the Reasoning LLM Adapter the Prompt Planner and the Art Director reason through.
	LLM       llm.Completer
	Adapter   *modeladapter.Adapter
	AssetsDir string       // the root of stored files (config.AssetsDir)
	Media     *media.Tools // reads the frame rate of every Take for its card; nil: the cards have none
	// Judge, when set, judges every Take as soon as it is made (AI Video Judge, F-13).
	Judge Judger
	// FreeBytes reports the free disk space at a path; it defaults to fsutil.FreeBytes.
	FreeBytes func(path string) (uint64, error)
	// Notify receives every progress notification. It MUST NOT block.
	Notify func(Progress)
	Logger *slog.Logger
}

// Orchestrator is the Generation Orchestrator. It is safe for concurrent use.
type Orchestrator struct {
	gens      *repository.GenerationRepository
	projects  *repository.ProjectRepository
	policies  *repository.PolicyRepository
	assets    *repository.AssetRepository
	repairs   *repository.RepairRepository
	brands    *repository.BrandRepository
	chat      Chat
	jobs      Jobs
	adapter   *modeladapter.Adapter
	planner   *promptplanner.Planner
	art       *artdirector.Director
	world     *repository.WorldRepository
	notes     *repository.NoteRepository
	extractor *stateextractor.Extractor
	files     fileStore
	judge     Judger
	freeDisk  func(path string) (uint64, error)
	notify    func(Progress)
	logger    *slog.Logger
}

// New returns an Orchestrator.
func New(d Deps) *Orchestrator {
	o := &Orchestrator{
		gens: d.Generations, projects: d.Projects, policies: d.Policies, assets: d.Assets, repairs: d.Repairs, brands: d.Brands, chat: d.Chat, jobs: d.Jobs, adapter: d.Adapter,
		planner: promptplanner.New(d.LLM), art: artdirector.New(d.LLM),
		files: fileStore{root: d.AssetsDir, media: d.Media}, freeDisk: d.FreeBytes, judge: d.Judge, notify: d.Notify, logger: d.Logger,
	}
	if o.freeDisk == nil {
		o.freeDisk = fsutil.FreeBytes
	}
	if o.logger == nil {
		o.logger = slog.New(slog.DiscardHandler)
	}
	o.world, o.notes, o.extractor = d.World, d.Notes, stateextractor.New(d.LLM, stateextractor.WithLogger(o.logger))
	if o.notify == nil {
		o.notify = func(Progress) {}
	}
	return o
}

// payload is the payload of a generate job.
type payload struct {
	Action      Action `json:"action"`
	ShotID      string `json:"shot_id"`
	Instruction string `json:"instruction,omitempty"`
	// Preserve are the parts of the Shot Spec the director asked to keep as they are.
	Preserve []domain.SpecPart `json:"preserve,omitempty"`
	// Spec is a Canonical Shot Spec already checked against LOCK brand constraints, so the job
	// does not plan a second one. Nil when the job keeps the Shot's spec or plans it itself.
	Spec *domain.ShotSpec `json:"spec,omitempty"`
	// NoteRefs are the production notes Spec was planned from.
	NoteRefs []domain.NoteRef `json:"note_refs,omitempty"`
	// CandidateSpecs are the Prompt Candidate specs already checked, in CandidateStrategies order.
	// The job uses these instead of planning them again; strategies past the end are planned in the job.
	CandidateSpecs []domain.ShotSpec `json:"candidate_specs,omitempty"`
}

// progressText is the content of the progress card of a job.
func progressText(a Action, shot domain.Shot) string {
	switch a {
	case ActionReferencePack:
		return fmt.Sprintf("Shot %d の Reference Pack（キャラクター・商品・場所・スタイル・ライティング・Keyframe）を作成しています", shot.OrderIndex+1)
	case ActionCandidates:
		return fmt.Sprintf("Shot %d の撮影戦略の候補を生成しています（Prompt Candidate → 動画生成 → Judge）", shot.OrderIndex+1)
	}
	return fmt.Sprintf("Shot %d を生成しています（Shot Spec の作成 → 動画生成）", shot.OrderIndex+1)
}

func (o *Orchestrator) enqueue(ctx context.Context, ref repository.ShotRef, p payload) (domain.Job, error) {
	claimed, err := o.gens.StartShot(ctx, ref.Shot.ID)
	if err != nil {
		return domain.Job{}, err
	}
	if !claimed {
		return domain.Job{}, ErrShotBusy
	}
	job, err := o.jobs.Enqueue(ctx, domain.JobGenerate, p)
	if err != nil {
		o.release(ctx, ref.Shot.ID)
		return domain.Job{}, fmt.Errorf("enqueue generate job: %w", err)
	}
	card := domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindProgress, Content: progressText(p.Action, ref.Shot), RefID: job.ID}
	if err := o.chat.Announce(ctx, ref.ProjectID, card); err != nil {
		o.logger.Error("post progress card failed", "shot_id", ref.Shot.ID, "job_id", job.ID, "err", err)
	}
	o.notify(Progress{ShotID: ref.Shot.ID, Action: p.Action, Stage: StageQueued})
	return job, nil
}

// release gives the Shot back (no generation is running). It works on a context that survives shutdown.
func (o *Orchestrator) release(ctx context.Context, shotID string) {
	if err := o.gens.SetShotState(context.WithoutCancel(ctx), shotID, domain.ShotPlanning); err != nil && !errors.Is(err, repository.ErrNotFound) {
		o.logger.Error("release shot failed", "shot_id", shotID, "err", err)
	}
}

// Handle is the job handler of domain.JobGenerate. Register it as NOT resumable: a generation
// costs money, so an interrupted one is failed and the user starts it again.
func (o *Orchestrator) Handle(ctx context.Context, job domain.Job) error {
	var p payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ShotID == "" || (p.Action != ActionGenerate && p.Action != ActionReferencePack && p.Action != ActionCandidates) {
		return fmt.Errorf("generate job %s: invalid payload", job.ID)
	}
	// The Shot was claimed (GENERATING) before this job was queued. Whichever way the handler ends, an
	// error before the work starts or a panic included, it gives the Shot back: only a restart
	// would release it otherwise, and every new generation of it would be refused as busy.
	released := false
	defer func() {
		if !released {
			o.release(ctx, p.ShotID)
		}
	}()
	ref, err := o.gens.GetShot(ctx, p.ShotID)
	if err != nil {
		return err
	}
	// The Shot may have been reset by Recover while this job waited in the queue.
	if err := o.gens.SetShotState(ctx, p.ShotID, domain.ShotGenerating); err != nil {
		return err
	}
	o.notify(Progress{ShotID: p.ShotID, Action: p.Action, Stage: StageGenerating, Message: "準備しています"})

	var done Progress
	switch p.Action {
	case ActionReferencePack:
		done, err = o.buildReferencePack(ctx, ref)
	case ActionCandidates:
		done, err = o.generateCandidates(ctx, ref, p.Instruction, p.Preserve, p.CandidateSpecs)
	default:
		done, err = o.generate(ctx, ref, p.Instruction, p.Preserve, p.Spec, p.NoteRefs)
	}
	released = true // fail and the success path give the Shot back themselves, before they tell the user
	if err != nil {
		return o.fail(ctx, ref, p.Action, err)
	}
	o.release(ctx, p.ShotID)
	done.ShotID, done.Action, done.Stage = p.ShotID, p.Action, StageDone
	o.notify(done)
	return nil
}

// parallel runs fn for i in [0, n) concurrently and returns the errors in order. A panic in fn is
// not propagated (it would end the goroutine, and the whole server, outside the job queue's
// recover): it is logged with its stack and becomes the error of that index, while the others
// finish.
func parallel(logger *slog.Logger, n int, fn func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("parallel task panicked", "index", i, "panic", r, "stack", string(debug.Stack()))
					errs[i] = fmt.Errorf("task %d panicked: %v", i, r)
				}
			}()
			errs[i] = fn(i)
		})
	}
	wg.Wait()
	return errs
}
