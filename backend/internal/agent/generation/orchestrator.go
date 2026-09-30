package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/artdirector"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/promptplanner"
	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
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
	// Brands gives the Safety / Brand Constraints the Prompt Planner follows (F-23). Optional.
	Brands *repository.BrandRepository
	// World holds the World State the State Extractor updates after each Shot.
	World *repository.WorldRepository
	Chat  Chat
	Jobs  Jobs
	// LLM is the Reasoning LLM Adapter the Prompt Planner and the Art Director reason through.
	LLM     llm.Completer
	Adapter *modeladapter.Adapter
	// AssetsDir is the root of stored files (config.AssetsDir).
	AssetsDir string
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
	brands    *repository.BrandRepository
	chat      Chat
	jobs      Jobs
	adapter   *modeladapter.Adapter
	planner   *promptplanner.Planner
	art       *artdirector.Director
	world     *repository.WorldRepository
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
		gens: d.Generations, projects: d.Projects, policies: d.Policies, assets: d.Assets, brands: d.Brands, chat: d.Chat, jobs: d.Jobs, adapter: d.Adapter,
		planner: promptplanner.New(d.LLM), art: artdirector.New(d.LLM),
		files: fileStore{root: d.AssetsDir}, freeDisk: d.FreeBytes, judge: d.Judge, notify: d.Notify, logger: d.Logger,
	}
	if o.freeDisk == nil {
		o.freeDisk = fsutil.FreeBytes
	}
	o.world, o.extractor = d.World, stateextractor.New(d.LLM)
	if o.logger == nil {
		o.logger = slog.New(slog.DiscardHandler)
	}
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

// Start begins generating the Shot: it claims the Shot (ErrShotBusy when it is already being
// generated), checks that at least one Take fits the budget (ErrBudgetExceeded) and queues the job.
// instruction is an optional extra direction ("もう少しカメラを寄せて").
func (o *Orchestrator) Start(ctx context.Context, shotID, instruction string) (domain.Job, error) {
	return o.StartDirected(ctx, shotID, instruction, nil)
}

// StartDirected is Start with the parts of the Shot Spec the director asked to preserve (the
// LOCKed parts are always preserved).
func (o *Orchestrator) StartDirected(ctx context.Context, shotID, instruction string, preserve []domain.SpecPart) (domain.Job, error) {
	return o.start(ctx, shotID, ActionGenerate, instruction, preserve)
}

// StartCandidates begins Prompt Candidate Generation for the Shot (F-08): the Prompt Planner writes
// candidate specs with different shooting strategies, each is generated, and the AI Video Judge
// picks the Best Take, all according to the Generation Mode of the Quality Policy (see
// generateCandidates). It fails like Start, and with ErrJudgeUnavailable when the Judge is off.
func (o *Orchestrator) StartCandidates(ctx context.Context, shotID, instruction string) (domain.Job, error) {
	if o.judge == nil {
		return domain.Job{}, ErrJudgeUnavailable
	}
	return o.start(ctx, shotID, ActionCandidates, instruction, nil)
}

func (o *Orchestrator) start(ctx context.Context, shotID string, action Action, instruction string, preserve []domain.SpecPart) (domain.Job, error) {
	ref, err := o.gens.GetShot(ctx, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	// A direction that contains a forbidden expression of a LOCK Brand constraint is refused up front.
	brand, err := o.brandConstraints(ctx, ref.ProjectID)
	if err != nil {
		return domain.Job{}, err
	}
	if err := brand.CheckText(instruction); err != nil {
		return domain.Job{}, err
	}
	// Candidates always rewrite the spec, so a LOCKed Prompt rules them out even without an instruction.
	if _, hasSpec := ref.Shot.CanonicalSpec(); hasSpec && (strings.TrimSpace(instruction) != "" || action == ActionCandidates) {
		locks, err := o.gens.ShotLocks(ctx, shotID)
		if err != nil {
			return domain.Job{}, err
		}
		if locks.Locked(domain.ParamPrompt) {
			return domain.Job{}, &repository.LockedParametersError{Parameters: []domain.LockParameter{domain.ParamPrompt}}
		}
	}
	policy, err := o.policies.Get(ctx, ref.ProjectID)
	if err != nil {
		return domain.Job{}, err
	}
	budget, err := o.gens.Budget(ctx, ref.ProjectID, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	strategy, err := o.decide(ctx, policy.Quality, ref.Shot, budget)
	if err != nil {
		return domain.Job{}, err
	}
	job, err := o.enqueue(ctx, ref, payload{Action: action, ShotID: shotID, Instruction: strings.TrimSpace(instruction), Preserve: preserve})
	if err != nil {
		return domain.Job{}, err
	}
	if action == ActionCandidates {
		strategy.Takes = plannedTakes(domain.ModeOf(policy.Quality), policy.Effective(), strategy)
	}
	o.warnLowDisk(ctx, ref, strategy)
	return job, nil
}

// decide is the generation strategy of the Shot: the Quality Policy, cost and model capabilities,
// and the Video Model when the user fixed one (Video Model MANUAL or LOCK).
func (o *Orchestrator) decide(ctx context.Context, quality domain.QualityPolicy, shot domain.Shot, budget repository.Budget) (Strategy, error) {
	locks, err := o.gens.ShotLocks(ctx, shot.ID)
	if err != nil {
		return Strategy{}, err
	}
	return DecideWithModel(o.adapter.Config(), quality, shot.Duration, budget, FixedModel(locks))
}

// FixedModel is the Video Model the user fixed for the Shot, or "" (AUTO, or no known model chosen).
func FixedModel(l domain.ParameterLocks) domain.ModelName {
	m := domain.ModelName(l.Text(domain.ParamVideoModel))
	if l.Mode(domain.ParamVideoModel) == domain.LockAuto || !slices.Contains(domain.VideoModels(), m) {
		return ""
	}
	return m
}

// bytesPerVideoSecond is the assumed size of one second of generated video (about 16 Mbit/s: the
// 1080p clips of the models). It is only used to warn before the disk fills up.
const bytesPerVideoSecond = 2 << 20

// warnLowDisk tells the user in the thread when the free space of the assets volume is smaller than
// what the Takes of the strategy are expected to take. Generation still starts: the estimate is rough
// (non-functional.md §4 "警告してから生成を開始する").
func (o *Orchestrator) warnLowDisk(ctx context.Context, ref repository.ShotRef, s Strategy) {
	need := uint64(float64(s.Takes) * s.DurationSeconds * bytesPerVideoSecond)
	free, err := o.freeDisk(o.files.root)
	if err != nil {
		o.logger.Warn("read free disk space failed", "err", err)
		return
	}
	if free >= need {
		return
	}
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("ディスクの空き容量が少ない可能性があります（Shot %d の生成に約 %s 必要な見込みに対し、空きは %s）。"+
			"不要な Take は設定画面（/projects/%s/settings）からアーカイブできます。", ref.Shot.OrderIndex+1, domain.FormatBytes(need), domain.FormatBytes(free), ref.ProjectID),
	}
	if err := o.chat.Announce(ctx, ref.ProjectID, notice); err != nil {
		o.logger.Error("post disk warning failed", "shot_id", ref.Shot.ID, "err", err)
	}
}

// StartReferencePack begins building (or rebuilding) the Reference Pack of the Shot.
func (o *Orchestrator) StartReferencePack(ctx context.Context, shotID string) (domain.Job, error) {
	ref, err := o.gens.GetShot(ctx, shotID)
	if err != nil {
		return domain.Job{}, err
	}
	return o.enqueue(ctx, ref, payload{Action: ActionReferencePack, ShotID: shotID})
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

// Recover puts Shots that were being generated when the app terminated abnormally back to PLANNING.
// Call it at start-up, before the job queue starts: the generate jobs of those Shots are failed by
// the queue (they are not resumable, so no paid call is repeated silently).
func (o *Orchestrator) Recover(ctx context.Context) (int, error) { return o.gens.ResetStaleShots(ctx) }

// Handle is the job handler of domain.JobGenerate. Register it as NOT resumable: a generation
// costs money, so an interrupted one is failed and the user starts it again.
func (o *Orchestrator) Handle(ctx context.Context, job domain.Job) error {
	var p payload
	if err := json.Unmarshal(job.Payload, &p); err != nil || p.ShotID == "" || (p.Action != ActionGenerate && p.Action != ActionReferencePack && p.Action != ActionCandidates) {
		return fmt.Errorf("generate job %s: invalid payload", job.ID)
	}
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
		done, err = o.generateCandidates(ctx, ref, p.Instruction)
	default:
		done, err = o.generate(ctx, ref, p.Instruction, p.Preserve)
	}
	if err != nil {
		return o.fail(ctx, ref, p.Action, err)
	}
	o.release(ctx, p.ShotID)
	done.ShotID, done.Action, done.Stage = p.ShotID, p.Action, StageDone
	o.notify(done)
	return nil
}

// fail tells the user why the job stopped and returns the error for the job record.
func (o *Orchestrator) fail(ctx context.Context, ref repository.ShotRef, a Action, err error) error {
	if ctx.Err() != nil {
		o.release(ctx, ref.Shot.ID)
		return err // shutting down: nobody is told
	}
	o.release(ctx, ref.Shot.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return err // the Shot is gone
	}
	o.logger.Error("generation failed", "shot_id", ref.Shot.ID, "action", a, "err", err)
	reason := FailureReason(err)
	what := "生成"
	if a == ActionReferencePack {
		what = "Reference Pack の作成"
	}
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Shot %d の%sに失敗しました: %s\n対処したら、もう一度実行してください。", ref.Shot.OrderIndex+1, what, reason),
	}
	if aerr := o.chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, notice); aerr != nil {
		o.logger.Error("post failure notice failed", "shot_id", ref.Shot.ID, "err", aerr)
	}
	o.notify(Progress{ShotID: ref.Shot.ID, Action: a, Stage: StageFailed, Message: reason})
	return errors.New(reason)
}

// FailureReason is the user-facing cause of a failure. Details stay in the log.
func FailureReason(err error) string {
	switch {
	case errors.Is(err, modeladapter.ErrNoAPIKey), errors.Is(err, llm.ErrNoAPIKey):
		return "生成モデルまたは LLM の API キーが設定されていません（設定画面でキーを登録してください）"
	case errors.Is(err, ErrBudgetExceeded):
		return "予算上限を超えるため生成できません"
	case errors.Is(err, domain.ErrBrandViolation):
		msg := "Brand / Safety Constraint に違反するため生成できません"
		if bv := new(domain.BrandViolationError); errors.As(err, &bv) {
			msg += "（" + bv.Message() + "）"
		}
		return msg
	case errors.Is(err, ErrJudgeUnavailable):
		return "AI Video Judge が利用できないため、候補から最良の Take を選べません"
	case errors.Is(err, ErrGenerationTimeLimit):
		return "生成が制限時間（Max generation time）を超えました。設定画面で制限時間を延ばすか、ポリシーを見直してください"
	case modeladapter.RateLimited(err):
		return "生成モデルのレート制限に達しました。しばらく待ってから再実行してください"
	case errors.Is(err, modeladapter.ErrCircuitOpen):
		return "生成モデルが一時的に利用できません。しばらく待ってから再実行してください"
	case errors.Is(err, modeladapter.ErrRejected):
		return "生成モデルがリクエストを拒否しました（コンテンツポリシー等）。指示を変えて再実行してください"
	case errors.Is(err, modeladapter.ErrTimeout), errors.Is(err, llm.ErrTimeout):
		return "生成モデルの応答がタイムアウトしました"
	case errors.Is(err, llm.ErrInvalidOutput), errors.Is(err, errInvalidPlan):
		return "AI の出力を制作に使える形で受け取れませんでした"
	}
	var pe *modeladapter.ProviderError
	if errors.As(err, &pe) {
		return "生成モデルの呼び出しに失敗しました"
	}
	var api *llm.APIError
	if errors.As(err, &api) {
		return "AI サービスの呼び出しに失敗しました"
	}
	return "内部エラーが発生しました"
}

// parallel runs fn for i in [0, n) concurrently and returns the errors in order.
func parallel(n int, fn func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { errs[i] = fn(i) })
	}
	wg.Wait()
	return errs
}
