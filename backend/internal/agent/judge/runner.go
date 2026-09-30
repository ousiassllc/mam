package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ErrBusy means the Take is already being judged (409, no double execution).
var ErrBusy = errors.New("judge: the take is already being judged")

// maxImageBytes bounds a stored image handed to a model.
const maxImageBytes = 20 << 20

// Deps are the collaborators of a Runner.
type Deps struct {
	Generations *repository.GenerationRepository
	Judges      *repository.JudgeRepository
	Assets      *repository.AssetRepository
	// Policies gives the Production Template of the Project, whose axes the Judge scores. Optional.
	Policies *repository.PolicyRepository
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
	Logger *slog.Logger
}

// Runner runs the AI Video Judge as background jobs: it gathers what the Judge needs about a Take,
// runs it, stores the result and tells the user. It is safe for concurrent use.
type Runner struct {
	Deps
}

// NewRunner returns a Runner.
func NewRunner(d Deps) *Runner {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Notify == nil {
		d.Notify = func(generation.Progress) {}
	}
	return &Runner{d}
}

type payload struct {
	TakeID string `json:"take_id"`
	ShotID string `json:"shot_id"`
}

// Start queues a Judge run on the Take (ErrBusy when one is already queued or running,
// repository.ErrNotFound for an unknown Take). Every run adds a result; earlier ones are kept.
func (r *Runner) Start(ctx context.Context, takeID string) (domain.Job, error) {
	ref, err := r.Judges.GetTake(ctx, takeID)
	if err != nil {
		return domain.Job{}, err
	}
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
	defer func() {
		if err := r.Judges.EndJudging(context.WithoutCancel(ctx), shotID, job.ID); err != nil {
			r.Logger.Error("release shot after judging failed", "shot_id", shotID, "err", err)
		}
	}()
	if err := r.Judges.BeginJudging(ctx, shotID); err != nil {
		return err
	}
	r.Notify(generation.Progress{ShotID: shotID, Action: generation.ActionJudge, Stage: generation.StageJudging, Message: "Take を評価しています"})

	res, err := r.judge(ctx, ref)
	if err != nil {
		return r.fail(ctx, ref, err)
	}
	if _, err := r.store(ctx, ref, res); err != nil {
		return err
	}
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
	return saved, nil
}

// pick marks the best judged Take of the Shot as its AI Pick: in AUTO a Take that passed the Judge
// also becomes the Shot's Take, in DIRECTOR the pick is only a recommendation. It returns the line
// of the chat notice about it ("" when the pick did not change or could not be made).
func (r *Runner) pick(ctx context.Context, ref repository.TakeRef) string {
	if r.Takes == nil || r.Projects == nil {
		return ""
	}
	ctx = context.WithoutCancel(ctx)
	project, err := r.Projects.Get(ctx, ref.ProjectID)
	if err != nil {
		r.Logger.Error("ai pick: read project failed", "project_id", ref.ProjectID, "err", err)
		return ""
	}
	pick, err := r.Takes.PickBest(ctx, ref.Take.ShotID, project.Mode == domain.ModeAuto)
	if err != nil {
		r.Logger.Error("ai pick failed", "shot_id", ref.Take.ShotID, "err", err)
		return ""
	}
	if !pick.Changed {
		return ""
	}
	if pick.Adopted {
		return fmt.Sprintf("\nAI Pick: Take %s を採用しました。", shortID(pick.TakeID))
	}
	return fmt.Sprintf("\nAI Pick: Take %s がおすすめです（Take 比較から選択できます）。", shortID(pick.TakeID))
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

// fail tells the user why judging stopped and returns the error for the job record.
func (r *Runner) fail(ctx context.Context, ref repository.TakeRef, err error) error {
	shotID := ref.Take.ShotID
	if ctx.Err() != nil {
		return err // shutting down: nobody is told
	}
	if errors.Is(err, repository.ErrNotFound) {
		return err
	}
	r.Logger.Error("judge failed", "take_id", ref.Take.ID, "err", err)
	reason := FailureReason(err)
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText,
		Content: fmt.Sprintf("Take %s の Judge に失敗しました: %s\n対処したら、もう一度 Judge を実行してください。", shortID(ref.Take.ID), reason),
	}
	if aerr := r.Chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, notice); aerr != nil {
		r.Logger.Error("post judge failure notice failed", "take_id", ref.Take.ID, "err", aerr)
	}
	r.Notify(generation.Progress{ShotID: shotID, Action: generation.ActionJudge, Stage: generation.StageFailed, Message: reason})
	return errors.New(reason)
}

// FailureReason is the user-facing cause of a failed Judge run. Details stay in the log.
func FailureReason(err error) string {
	if errors.Is(err, ErrNoFFmpeg) {
		return "ffmpeg / ffprobe が見つかりません（インストールして PATH に追加してください）"
	}
	return generation.FailureReason(err)
}

// input gathers what the Judge needs: the spec the Take was generated from (with its Reference
// Pack), the neighbouring Shots, the LOCKed parameters and the face references of the Character.
func (r *Runner) input(ctx context.Context, ref repository.TakeRef) (Input, error) {
	snapshot := domain.Shot{ShotSpec: ref.SpecSnapshot}
	spec, ok := snapshot.CanonicalSpec()
	if !ok {
		return Input{}, fmt.Errorf("take %s: its prompt version has no Canonical Shot Spec", ref.Take.ID)
	}
	path, err := r.videoPath(ref.Take)
	if err != nil {
		return Input{}, err
	}
	in := Input{VideoPath: path, Spec: spec}
	if r.Policies != nil {
		policy, err := r.Policies.Get(ctx, ref.ProjectID)
		if err != nil {
			return Input{}, err
		}
		in.Template = policy.Template
	}

	shots, err := r.Generations.ListProjectShots(ctx, ref.ProjectID)
	if err != nil {
		return Input{}, err
	}
	in.Previous, in.Next = neighbours(shots, ref.Take.ShotID)

	references := snapshot.References()
	for _, rf := range references {
		if img, ok := r.image(ctx, ref.ProjectID, rf.AssetID); ok {
			in.References = append(in.References, RefImage{Kind: rf.Kind, Name: rf.Name, MimeType: img.mime, Data: img.data})
		}
	}

	locks, err := r.Judges.Locks(ctx, ref.ProjectID)
	if err != nil {
		return Input{}, err
	}
	world, err := r.Generations.WorldContext(ctx, ref.ProjectID)
	if err != nil {
		return Input{}, err
	}
	in.Locked = lockedDimensions(spec, world, locks)

	if spec.Character != nil {
		if subject, ok := r.faceSubject(ctx, ref.ProjectID, *spec.Character, world, references); ok {
			in.FaceSubjects = []FaceSubject{subject}
		}
	}
	return in, nil
}

// neighbours returns the specs of the Shots before and after shotID in story order.
func neighbours(shots []repository.ShotRef, shotID string) (prev, next *domain.ShotSpec) {
	for i, s := range shots {
		if s.Shot.ID != shotID {
			continue
		}
		if i > 0 {
			if spec, ok := shots[i-1].Shot.CanonicalSpec(); ok {
				prev = &spec
			}
		}
		if i+1 < len(shots) {
			if spec, ok := shots[i+1].Shot.CanonicalSpec(); ok {
				next = &spec
			}
		}
		return prev, next
	}
	return nil, nil
}

// dimensionsOfLock maps the LOCKed attributes of a Character, Product or Location to the Judge
// dimensions that check them (spec/spec.md §9.3): a deviation in such a dimension is a LOCK
// violation and fails the Take whatever the score.
var dimensionsOfLock = map[domain.WorldTarget]map[string][]domain.JudgeDimension{
	domain.TargetCharacter: {
		domain.LockParamIdentity: {domain.DimCharacterIdentity, domain.DimFace},
		"wardrobe":               {domain.DimCharacterConsistency},
		"hairstyle":              {domain.DimCharacterConsistency},
	},
	domain.TargetProduct: {
		domain.LockParamGeometry: {domain.DimProductGeometry},
		domain.LockParamLabel:    {domain.DimLogoTextAccuracy},
		"orientation":            {domain.DimProductAccuracy},
		"material":               {domain.DimProductAccuracy},
		"condition":              {domain.DimProductAccuracy},
	},
	domain.TargetLocation: {
		"time":                          {domain.DimLighting},
		"lighting_state":                {domain.DimLighting},
		"weather":                       {domain.DimBackground},
		domain.LockFieldLayoutReference: {domain.DimBackground},
	},
}

// lockedDimensions maps the LOCKs that apply to the Shot to Judge dimensions. The Character,
// Product and Location of the Shot are found in the World State by ID or name (the Shot Spec
// uses either); an attribute is LOCKed by a lock row of that entity, and Character Identity and
// Product Geometry also by the Shot Spec itself ("locked").
func lockedDimensions(spec domain.ShotSpec, world domain.WorldContext, locks domain.LockSet) map[domain.JudgeDimension]bool {
	out := map[domain.JudgeDimension]bool{}
	mark := func(target domain.WorldTarget, id string) {
		for field, dims := range dimensionsOfLock[target] {
			if locks.Locked(target, id, field) {
				for _, d := range dims {
					out[d] = true
				}
			}
		}
	}
	if c := spec.Character; c != nil {
		id := c.ID
		if w, ok := world.Character(c.ID); ok {
			id = w.ID
		}
		mark(domain.TargetCharacter, id)
		if c.Identity == "locked" {
			out[domain.DimCharacterIdentity], out[domain.DimFace] = true, true
		}
	}
	if p := spec.Product; p != nil {
		id := p.ID
		if w, ok := world.Product(p.ID); ok {
			id = w.ID
		}
		mark(domain.TargetProduct, id)
		if p.Geometry == "locked" {
			out[domain.DimProductGeometry] = true
		}
	}
	if l := spec.Location; l != nil {
		id := l.ID
		if w, ok := world.Location(l.ID); ok {
			id = w.ID
		}
		mark(domain.TargetLocation, id)
	}
	return out
}

// faceSubject is the Character of the Shot with its reference faces: the face_reference of the
// Series canon the Character was copied from (so that the face is measured against the same
// baseline in every Episode, F-29), the Character's own face_reference asset and the Character
// images of the Reference Pack that carry its name. Images that are the same file count once: an
// Episode's copy of the canon face must not weigh twice in the calibration of the thresholds.
func (r *Runner) faceSubject(ctx context.Context, projectID string, actor domain.SpecActor, world domain.WorldContext, refs []domain.Reference) (FaceSubject, bool) {
	// The Shot Spec names the Character by ID or by name, whichever the Prompt Planner wrote.
	var character *domain.WorldCharacter
	for i, c := range world.Characters {
		if c.ID == actor.ID || strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(actor.ID)) {
			character = &world.Characters[i]
			break
		}
	}
	subject := FaceSubject{ID: actor.ID}
	if character == nil {
		return subject, false
	}
	seen := map[string]bool{}
	add := func(img storedImage, ok bool) {
		if ok && !seen[img.path] {
			seen[img.path] = true
			subject.ReferenceImages = append(subject.ReferenceImages, img.data)
		}
	}
	if id := character.SeriesFaceReferenceAssetID; id != "" {
		add(r.canonImage(ctx, id))
	}
	if id := character.FaceReferenceAssetID; id != "" {
		add(r.image(ctx, projectID, id))
	}
	for _, rf := range refs {
		if rf.Kind == domain.RefCharacter && strings.EqualFold(strings.TrimSpace(rf.Name), strings.TrimSpace(character.Name)) {
			add(r.image(ctx, projectID, rf.AssetID))
		}
	}
	return subject, len(subject.ReferenceImages) > 0
}

type storedImage struct {
	data []byte
	mime string
	path string // the file, as in asset.file_path
}

// image reads the image of an asset. A missing or oversized file is skipped: a Reference Pack can
// be rebuilt, so a lost image must not stop the Judge.
func (r *Runner) image(ctx context.Context, projectID, assetID string) (storedImage, bool) {
	asset, err := r.Assets.Get(ctx, projectID, assetID)
	return r.read(assetID, asset, err)
}

// canonImage reads the image of a Series canon asset, which belongs to the Project the Series was
// made from, not to the Project being judged.
func (r *Runner) canonImage(ctx context.Context, assetID string) (storedImage, bool) {
	asset, err := r.Assets.GetAny(ctx, assetID)
	return r.read(assetID, asset, err)
}

// read reads the file of an asset that was looked up with err.
func (r *Runner) read(assetID string, asset domain.Asset, err error) (storedImage, bool) {
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			r.Logger.Warn("reference asset unavailable", "asset_id", assetID, "err", err)
		}
		return storedImage{}, false
	}
	abs, err := r.assetPath(asset.FilePath)
	if err != nil {
		r.Logger.Warn("reference asset path rejected", "asset_id", assetID, "err", err)
		return storedImage{}, false
	}
	if info, err := os.Stat(abs); err != nil || info.Size() > maxImageBytes {
		return storedImage{}, false
	}
	data, err := os.ReadFile(abs) // #nosec G304 -- inside the assets directory (assetPath)
	if err != nil {
		r.Logger.Warn("reference image unreadable", "asset_id", assetID, "err", err)
		return storedImage{}, false
	}
	return storedImage{data: data, mime: asset.MimeType, path: asset.FilePath}, true
}

// videoPath is the file of the Take.
func (r *Runner) videoPath(t domain.Take) (string, error) {
	if t.FilePath == "" {
		return "", fmt.Errorf("take %s has no video file", t.ID)
	}
	abs, err := r.assetPath(t.FilePath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("take %s: %w", t.ID, err)
	}
	return abs, nil
}

// assetPath resolves a path stored in the database inside the assets directory.
func (r *Runner) assetPath(rel string) (string, error) {
	if r.AssetsDir == "" {
		return "", errors.New("assets directory is not configured")
	}
	abs := filepath.Join(r.AssetsDir, filepath.FromSlash(rel))
	if within, err := filepath.Rel(r.AssetsDir, abs); err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the assets directory", rel)
	}
	return abs, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Summary is the chat notice of a Judge result: the verdict and the Issues.
func Summary(shotNumber int, takeID string, res domain.JudgeResult) string {
	var b strings.Builder
	verdict := "PASS"
	if res.Result == domain.JudgeFail {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "Shot %d · Take %s の Judge 結果: %s（信頼度 %.2f）", shotNumber, shortID(takeID), verdict, res.OverallConfidence)
	for _, is := range res.Issues {
		fmt.Fprintf(&b, "\n・[%s] %s", is.Severity, is.Type)
		if is.TimeRange != "" {
			b.WriteString(" " + is.TimeRange)
		}
		if is.LockViolation {
			b.WriteString("（LOCK 違反）")
		}
	}
	switch res.Metrics.FaceIdentityStatus {
	case domain.FaceUnavailable:
		b.WriteString("\n顔の同一性の数値評価は利用できないため、定性評価のみで判定しました。")
	case domain.FaceMeasured:
		for _, f := range res.Metrics.FaceIdentity {
			if !f.Evaluated {
				b.WriteString("\n顔を検出できないフレームが多いため、顔の同一性は定性評価のみで判定しました。")
				break
			}
		}
	}
	if rs := res.RepairStrategy; rs != nil {
		fmt.Fprintf(&b, "\n推奨する修復: %s", rs.Action)
	}
	return b.String()
}
