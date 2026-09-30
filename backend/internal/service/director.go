package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/repair"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// DirectorService runs DIRECTOR MODE (F-02, F-05, F-24): the AUTO ⇄ DIRECTOR switch, the
// AUTO / MANUAL / LOCK setting of parameters, the manual edit of the Canonical Shot Spec and the
// Command Interpreter that turns a direction into operations on one Shot.
type DirectorService struct {
	*core
	gens     *repository.GenerationRepository
	locks    *repository.LockRepository
	versions *repository.VersionRepository
	// orch starts the regeneration of a Shot; nil when generation is not configured.
	orch *generation.Orchestrator
	// interpreter reads directions; nil when no Reasoning LLM is configured.
	interpreter *commandinterpreter.Interpreter
	// branch makes the Branches of a "create_branch" operation; set by New.
	branch *BranchService
	// takes finds the Take a Partial Regeneration fixes; repair makes it (nil without ffmpeg, LLM
	// or Model Adapter).
	takes  *repository.TakeRepository
	repair *repair.Agent
}

// SetMode switches the Project between AUTO and DIRECTOR (PUT /projects/:id/mode) and tells the
// thread. Back in AUTO the pipeline looks at the Project again, so the stages after the ones the
// director fixed resume. The mode is kept: it is a column of the Project.
func (s *DirectorService) SetMode(ctx context.Context, projectID string, mode domain.Mode) (domain.Project, error) {
	if !mode.Valid() {
		return domain.Project{}, invalid("mode", "モードが正しくありません")
	}
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	if project.Mode == mode {
		return project, nil
	}
	if err := s.applyMode(ctx, projectID, mode); err != nil {
		return domain.Project{}, err
	}
	if mode == domain.ModeAuto {
		s.kick(ctx, projectID)
	}
	return s.getProject(ctx, projectID)
}

func modeNotice(mode domain.Mode) string {
	if mode == domain.ModeDirector {
		return "DIRECTOR MODE にしました。指示した箇所だけを直して待機します（AI が先に進めることはありません）。"
	}
	return "AUTO MODE に戻しました。この先の工程は AI が続けます。"
}

func (s *DirectorService) applyMode(ctx context.Context, projectID string, mode domain.Mode) error {
	msgs := assignIDs([]domain.ChatMessage{assistantText(modeNotice(mode))})
	if err := s.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: msgs, Mode: &mode}); err != nil {
		return notFound(err)
	}
	s.publishCreated(projectID, msgs)
	return nil
}

func checkLockInput(p domain.LockParameter, mode domain.LockMode, value string, valid bool) error {
	if !valid {
		return invalid("parameter", "このパラメータは設定できません")
	}
	if !mode.Valid() {
		return invalid("mode", "AUTO / MANUAL / LOCK のいずれかを選んでください")
	}
	if p != domain.ParamVideoModel {
		return nil
	}
	if value != "" && !slices.Contains(domain.VideoModels(), domain.ModelName(value)) {
		return invalid("value", "動画モデルが正しくありません")
	}
	if mode == domain.LockManual && value == "" {
		return invalid("value", "MANUAL では動画モデルを選んでください")
	}
	return nil
}

// SetShotLock sets one parameter of the Shot to AUTO, MANUAL or LOCK (PUT /shots/:id/locks) and
// returns the Shot's settings. value is the Video Model chosen with MANUAL or LOCK; LOCK without
// one freezes the model of the Shot's latest Take. Replacing the value of a LOCKed parameter that
// stays LOCKed is ErrLocked.
func (s *DirectorService) SetShotLock(ctx context.Context, shotID string, p domain.LockParameter, mode domain.LockMode, value string) (domain.ParameterLocks, error) {
	value = strings.TrimSpace(value)
	if err := checkLockInput(p, mode, value, p.ValidForShot()); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if value != "" && mode != domain.LockAuto {
		raw, _ = json.Marshal(value)
	}
	if err := s.locks.SetShotLock(ctx, shotID, p, mode, raw); err != nil {
		return nil, lockedError(notFound(err))
	}
	return s.locks.ShotLocks(ctx, shotID)
}

// SetProjectLock sets Story or Music of the Project (PUT /projects/:id/locks). A LOCKed Story is
// never changed by the State Extractor.
func (s *DirectorService) SetProjectLock(ctx context.Context, projectID string, p domain.LockParameter, mode domain.LockMode) (domain.ParameterLocks, error) {
	if err := checkLockInput(p, mode, "", p.ValidForProject()); err != nil {
		return nil, err
	}
	if err := s.locks.SetProjectLock(ctx, projectID, p, mode); err != nil {
		return nil, notFound(err)
	}
	return s.locks.ProjectLocks(ctx, projectID)
}

// ProjectLocks returns the setting of Story and Music.
func (s *DirectorService) ProjectLocks(ctx context.Context, projectID string) (domain.ParameterLocks, error) {
	return s.locks.ProjectLocks(ctx, projectID)
}

// Inspector is the Shot Inspector (GET /shots/:id): the Shot, its Canonical Shot Spec, Take list
// and the AUTO / MANUAL / LOCK setting of each parameter.
type Inspector struct {
	Project domain.Project
	Scene   domain.Scene
	Shot    domain.Shot
	Spec    *domain.ShotSpec // nil until a spec exists
	Takes   []domain.Take
	Locks   domain.ParameterLocks
	// Pack is the Reference Pack of the Shot and Library the images of the Project that can be added to it (F-11).
	Pack    []domain.Reference
	Library []domain.ReferenceItem
	// SpecInput and SpecError hold a rejected manual edit of the spec.
	SpecInput string
	SpecError string
	// Versions are the saved versions of the Shot's spec, newest first (F-28).
	Versions []domain.VersionEntry
}

// SpecText is the Canonical Shot Spec as the edit form shows it (indented JSON).
func (i Inspector) SpecText() string {
	if i.SpecInput != "" {
		return i.SpecInput
	}
	if i.Spec == nil {
		return ""
	}
	b, err := json.MarshalIndent(i.Spec, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// Inspector returns the Shot Inspector.
func (s *DirectorService) Inspector(ctx context.Context, shotID string) (Inspector, error) {
	ref, err := s.gens.GetShot(ctx, shotID)
	if err != nil {
		return Inspector{}, notFound(err)
	}
	project, err := s.getProject(ctx, ref.ProjectID)
	if err != nil {
		return Inspector{}, err
	}
	takes, err := s.plans.ListTakes(ctx, ref.Scene.ID)
	if err != nil {
		return Inspector{}, err
	}
	locks, err := s.locks.ShotLocks(ctx, shotID)
	if err != nil {
		return Inspector{}, err
	}
	library, err := s.references.List(ctx, ref.ProjectID, repository.ReferenceFilter{})
	if err != nil {
		return Inspector{}, err
	}
	versions, err := s.versions.ShotVersions(ctx, shotID)
	if err != nil {
		return Inspector{}, notFound(err)
	}
	view := Inspector{Project: project, Scene: ref.Scene, Shot: ref.Shot, Takes: takes[shotID], Locks: locks, Pack: ref.Shot.References(), Library: library, Versions: versions}
	if spec, ok := ref.Shot.CanonicalSpec(); ok {
		view.Spec = &spec
	}
	return view, nil
}

// EditSpec stores a Canonical Shot Spec that the user edited by hand (PUT /shots/:id/shot-spec).
// text is the spec as JSON (the Inspector's form). A spec that does not parse or fails
// domain.ShotSpec.Validate is a ValidationError (the returned Inspector keeps the input and the
// reason); a change to a LOCKed parameter is ErrLocked and writes nothing; a Shot being generated is ErrBusy.
func (s *DirectorService) EditSpec(ctx context.Context, shotID, text string) (Inspector, error) {
	ref, err := s.gens.GetShot(ctx, shotID)
	if err != nil {
		return Inspector{}, notFound(err)
	}
	reject := func(msg string) (Inspector, error) {
		view, err := s.Inspector(ctx, shotID)
		if err != nil {
			return Inspector{}, err
		}
		view.SpecInput, view.SpecError = text, msg
		return view, invalid("shot_spec", "%s", msg)
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.DisallowUnknownFields()
	var spec domain.ShotSpec
	if err := dec.Decode(&spec); err != nil {
		return reject("Shot Spec を JSON として読み取れません: " + err.Error())
	}
	spec.Duration = domain.FormatDuration(ref.Shot.Duration)
	if err := spec.Validate(ref.Shot.Duration); err != nil {
		return reject("Shot Spec が正しくありません: " + err.Error())
	}
	if _, err := s.gens.SaveEditedSpec(ctx, shotID, spec); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return Inspector{}, ErrBusy
		}
		return Inspector{}, lockedError(notFound(err))
	}
	return s.Inspector(ctx, shotID)
}

// DirectorCommandResult is what a direction did.
type DirectorCommandResult struct {
	Inspector Inspector
	// Started is true when a regeneration was queued (HTTP 202).
	Started bool
}

// DirectorCommand carries out a natural language direction for a Shot whose target is known
// (POST /shots/:id/director-command). The message and the result are recorded in the thread. It
// fails with ErrLocked, and does nothing, when the direction changes a LOCKed parameter.
func (s *DirectorService) DirectorCommand(ctx context.Context, shotID, message string) (DirectorCommandResult, error) {
	message = strings.TrimSpace(message)
	if err := validateMessage(message, false); err != nil {
		return DirectorCommandResult{}, invalid("content", "指示を入力してください（%d 文字以内）", MaxMessageRunes)
	}
	ref, err := s.gens.GetShot(ctx, shotID)
	if err != nil {
		return DirectorCommandResult{}, notFound(err)
	}
	if s.interpreter == nil {
		return DirectorCommandResult{}, ErrUnavailable
	}
	project, err := s.getProject(ctx, ref.ProjectID)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	shots, err := s.gens.StoryShots(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	cmd, err := s.interpret(ctx, message, shots, &ref)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	locks, err := s.locks.ShotLocks(ctx, shotID)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	if err := lockedRegeneration(locks, cmd.Operations); err != nil {
		return DirectorCommandResult{}, err // nothing is recorded or done
	}
	user := assignIDs([]domain.ChatMessage{userText(message, "", nil)})
	if err := s.commitMessages(ctx, project.ID, user); err != nil {
		return DirectorCommandResult{}, err
	}
	res, err := s.execute(ctx, project, &ref, cmd, true)
	if err != nil {
		return DirectorCommandResult{}, err
	}
	if err := s.commitMessages(ctx, project.ID, assignIDs([]domain.ChatMessage{assistantText(res.text())})); err != nil {
		return DirectorCommandResult{}, err
	}
	view, err := s.Inspector(ctx, shotID)
	return DirectorCommandResult{Inspector: view, Started: res.started}, err
}

func (s *DirectorService) commitMessages(ctx context.Context, projectID string, msgs []domain.ChatMessage) error {
	if err := s.projects.Apply(ctx, repository.Turn{ProjectID: projectID, Messages: msgs}); err != nil {
		return notFound(err)
	}
	s.publishCreated(projectID, msgs)
	return nil
}

// wants reports whether a chat message is a direction for the Command Interpreter: a message that asks
// the team to remember something (F-31, in any mode), or, in DIRECTOR MODE past the Brief, any message.
// A message that answers an open card never is.
func (s *DirectorService) wants(project domain.Project, target *domain.ChatMessage, content string) bool {
	if content == "" || (target != nil && isAnswerable(target.Kind)) {
		return false
	}
	if domain.AsksToRemember(content) {
		return true
	}
	return project.Mode == domain.ModeDirector && project.State != domain.StateDraft
}

// Chat interprets a message of the thread (already stored) and carries it out. The result is the AI
// message to append to the thread: what was done, what was refused, or what to ask. Failures the user
// can act on are reported in the message; only unexpected errors are returned.
func (s *DirectorService) Chat(ctx context.Context, project domain.Project, target *domain.ChatMessage, content string) (domain.ChatMessage, error) {
	if s.interpreter == nil {
		return assistantText("LLM が設定されていないため、指示を解釈できません（設定画面で API キーを登録してください）。"), nil
	}
	shots, err := s.gens.ListProjectShots(ctx, project.ID)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	ref, err := s.contextShot(ctx, project.ID, target, shots)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	cmd, err := s.interpret(ctx, content, shots, ref)
	if err != nil {
		return assistantText("指示を読み取れませんでした: " + generation.FailureReason(err)), nil
	}
	if project.Mode != domain.ModeDirector {
		// Outside DIRECTOR MODE only a request to remember something is carried out: AUTO MODE does
		// not take Shot directions from the chat.
		return s.rememberOnly(ctx, project, cmd)
	}
	if cmd.Question != "" {
		return assistantText(cmd.Question), nil
	}
	if shot := shotNamed(shots, cmd.Scene, cmd.Shot); shot != nil {
		ref = shot
	}
	res, err := s.execute(ctx, project, ref, cmd, false)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	return assistantText(res.text()), nil
}

// rememberOnly carries out the create_note operations of a Command (AUTO MODE).
func (s *DirectorService) rememberOnly(ctx context.Context, project domain.Project, cmd commandinterpreter.Command) (domain.ChatMessage, error) {
	cmd.Operations = slices.DeleteFunc(cmd.Operations, func(op commandinterpreter.Operation) bool { return op.Type != commandinterpreter.OpCreateNote })
	if len(cmd.Operations) == 0 {
		return assistantText("メモとして残せる内容が見つかりませんでした。覚えておきたい内容を、もう一度教えてください。"), nil
	}
	res, err := s.execute(ctx, project, nil, cmd, false)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	return assistantText(res.text()), nil
}

// remember keeps what the director asked to remember as a note of the Project (source chat).
func (s *DirectorService) remember(ctx context.Context, projectID string, op commandinterpreter.Operation) (string, error) {
	n, err := s.notes.createProjectNote(ctx, projectID,
		NoteInput{Title: op.Title, Content: op.Content, Category: string(op.Category)}, domain.NoteSourceChat)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return "メモに残せませんでした: " + ve.Message, nil
		}
		return "", err
	}
	return fmt.Sprintf("メモに残しました:「%s」（%s）。以降の脚本・構成・Shot の設計で参照します。", n.Version.Title, n.Category.Label()), nil
}

// contextShot resolves the Shot a message is about without the words of the message: the Take card
// it replies to, else the latest Take card of the thread, else the only Shot of the Project.
func (s *DirectorService) contextShot(ctx context.Context, projectID string, target *domain.ChatMessage, shots []repository.ShotRef) (*repository.ShotRef, error) {
	cards := []*domain.ChatMessage{target}
	if target == nil || target.Kind != domain.KindTake {
		latest, err := s.chat.LatestOfKind(ctx, projectID, domain.KindTake)
		if err != nil {
			return nil, err
		}
		cards = []*domain.ChatMessage{target, latest}
	}
	for _, m := range cards {
		if m == nil || m.Kind != domain.KindTake || m.RefID == "" {
			continue
		}
		ref, err := s.gens.ShotOfTake(ctx, projectID, m.RefID)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &ref, nil
	}
	if len(shots) == 1 {
		return &shots[0], nil
	}
	return nil, nil
}

// shotNamed finds the Shot by the numbers a message names (1-based); nil when it names none.
func shotNamed(shots []repository.ShotRef, scene, shot int) *repository.ShotRef {
	if scene < 1 || shot < 1 {
		return nil
	}
	for i := range shots {
		if shots[i].Scene.OrderIndex+1 == scene && shots[i].Shot.OrderIndex+1 == shot {
			return &shots[i]
		}
	}
	return nil
}

func (s *DirectorService) interpret(ctx context.Context, message string, shots []repository.ShotRef, target *repository.ShotRef) (commandinterpreter.Command, error) {
	in := commandinterpreter.Input{Message: message}
	for _, sh := range shots {
		in.Shots = append(in.Shots, optionOf(sh))
	}
	if target != nil {
		opt := optionOf(*target)
		in.Target = &opt
		if spec, ok := target.Shot.CanonicalSpec(); ok {
			in.Spec = &spec
		}
		locks, err := s.locks.ShotLocks(ctx, target.Shot.ID)
		if err != nil {
			return commandinterpreter.Command{}, err
		}
		in.Locks = locks
	}
	return s.interpreter.Interpret(ctx, in)
}

func optionOf(sh repository.ShotRef) commandinterpreter.ShotOption {
	return commandinterpreter.ShotOption{Scene: sh.Scene.OrderIndex + 1, Shot: sh.Shot.OrderIndex + 1, Objective: sh.Shot.Objective}
}

// outcome is what the operations of one direction did, as lines for the thread.
type outcome struct {
	lines   []string
	started bool
}

func (o outcome) text() string {
	if len(o.lines) == 0 {
		return "指示として実行できる内容が見つかりませんでした。対象の Shot と変えたい点（例:「Scene 1 の Shot 2 のカメラを固定して」）を教えてください。"
	}
	return strings.Join(o.lines, "\n")
}

const askWhichShot = "どの Shot への指示ですか？ Take カードに返信するか、「Scene 1 の Shot 2」のように指定してください。"

// execute carries out the operations of a Command: settings first (so that "カメラは固定して、表情だけ
// 変えて" locks before it regenerates), then regenerations, then the mode switch. strict makes a
// regeneration that would change a LOCKed parameter fail with ErrLocked before anything is done;
// otherwise it is left out and reported.
func (s *DirectorService) execute(ctx context.Context, project domain.Project, ref *repository.ShotRef, cmd commandinterpreter.Command, strict bool) (outcome, error) {
	var out outcome
	if cmd.Question != "" {
		return outcome{lines: []string{cmd.Question}}, nil
	}
	needsShot := slices.ContainsFunc(cmd.Operations, func(op commandinterpreter.Operation) bool {
		return op.Regenerates() || op.Type == commandinterpreter.OpCreateBranch || (op.Type == commandinterpreter.OpSetLock && op.Parameter.ValidForShot())
	})
	if needsShot && ref == nil {
		return outcome{lines: []string{askWhichShot}}, nil
	}

	var locks domain.ParameterLocks
	if ref != nil {
		var err error
		if locks, err = s.locks.ShotLocks(ctx, ref.Shot.ID); err != nil {
			return out, err
		}
		if strict {
			if err := lockedRegeneration(locks, cmd.Operations); err != nil {
				return out, err
			}
		}
	}

	for _, op := range cmd.Operations {
		if op.Type != commandinterpreter.OpSetLock {
			continue
		}
		line, err := s.setLock(ctx, project.ID, ref, op)
		if err != nil {
			return out, err
		}
		out.lines = append(out.lines, line)
	}
	if ref != nil {
		var err error
		if locks, err = s.locks.ShotLocks(ctx, ref.Shot.ID); err != nil {
			return out, err
		}
	}
	for _, op := range cmd.Operations {
		switch op.Type {
		case commandinterpreter.OpRegenerate, commandinterpreter.OpPartialRegenerate:
			line, started, err := s.regenerate(ctx, ref, locks, op)
			if err != nil {
				return out, err
			}
			out.lines = append(out.lines, line)
			out.started = out.started || started
		case commandinterpreter.OpCreateBranch:
			lines, err := s.branch.Fork(ctx, *ref, op.PointType, op.Count, op.Directions)
			out.lines = append(out.lines, lines...)
			if err != nil {
				return out, err
			}
		case commandinterpreter.OpUnsupported:
			reason := op.Reason
			if reason == "" {
				reason = "その操作にはまだ対応していません"
			}
			out.lines = append(out.lines, reason)
		}
	}
	for _, op := range cmd.Operations {
		if op.Type != commandinterpreter.OpCreateNote {
			continue
		}
		line, err := s.remember(ctx, project.ID, op)
		if err != nil {
			return out, err
		}
		out.lines = append(out.lines, line)
	}
	for _, op := range cmd.Operations {
		if op.Type == commandinterpreter.OpSetMode {
			if err := s.applyMode(ctx, project.ID, op.ProjectMode); err != nil {
				return out, err
			}
			if op.ProjectMode == domain.ModeAuto {
				s.kick(ctx, project.ID)
			}
		}
	}
	return out, nil
}

// lockedRegeneration returns ErrLocked when a regeneration of ops changes a parameter that is
// LOCKed once the ops' own settings are applied.
func lockedRegeneration(locks domain.ParameterLocks, ops []commandinterpreter.Operation) error {
	effective := domain.ParameterLocks{}
	for p, v := range locks {
		effective[p] = v
	}
	for _, op := range ops {
		if op.Type == commandinterpreter.OpSetLock && op.Parameter.ValidForShot() {
			effective[op.Parameter] = domain.LockSetting{Mode: op.Mode}
		}
	}
	var hit []domain.LockParameter
	for _, op := range ops {
		if op.Regenerates() {
			hit = append(hit, lockedParameters(effective, op)...)
		}
	}
	if len(hit) > 0 {
		return &LockedError{Parameters: hit}
	}
	return nil
}

// lockedParameters lists the LOCKed parameters that the regeneration changes: the parameter of
// each changed part, and Prompt, which covers the whole spec.
func lockedParameters(locks domain.ParameterLocks, op commandinterpreter.Operation) []domain.LockParameter {
	var hit []domain.LockParameter
	for _, part := range op.Changes {
		if p, ok := part.Parameter(); ok && locks.Locked(p) {
			hit = append(hit, p)
		}
	}
	if locks.Locked(domain.ParamPrompt) && !slices.Contains(hit, domain.ParamPrompt) {
		hit = append(hit, domain.ParamPrompt)
	}
	return hit
}

func (s *DirectorService) setLock(ctx context.Context, projectID string, ref *repository.ShotRef, op commandinterpreter.Operation) (string, error) {
	if op.Parameter.ValidForProject() {
		if _, err := s.SetProjectLock(ctx, projectID, op.Parameter, op.Mode); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s を %s にしました。", op.Parameter.Label(), op.Mode.Label()), nil
	}
	if op.Parameter == domain.ParamVideoModel && op.Mode == domain.LockManual && op.Value == "" {
		return "Video Model を MANUAL にするにはモデル名（kling-3.0 / seedance-2.0）を指定してください。", nil
	}
	if _, err := s.SetShotLock(ctx, ref.Shot.ID, op.Parameter, op.Mode, op.Value); err != nil {
		if errors.Is(err, ErrLocked) {
			return fmt.Sprintf("Shot %d の %s は LOCK 中の値を別の値に変えられません。先に AUTO か MANUAL に切り替えてください。", ref.Shot.OrderIndex+1, op.Parameter.Label()), nil
		}
		return "", err
	}
	return fmt.Sprintf("Scene %d / Shot %d の %s を %s にしました。", ref.Scene.OrderIndex+1, ref.Shot.OrderIndex+1, op.Parameter.Label(), op.Mode.Label()), nil
}

func (s *DirectorService) regenerate(ctx context.Context, ref *repository.ShotRef, locks domain.ParameterLocks, op commandinterpreter.Operation) (string, bool, error) {
	label := fmt.Sprintf("Scene %d / Shot %d", ref.Scene.OrderIndex+1, ref.Shot.OrderIndex+1)
	if hit := lockedParameters(locks, op); len(hit) > 0 {
		names := make([]string, len(hit))
		for i, p := range hit {
			names[i] = p.Label()
		}
		return fmt.Sprintf("%s の %s は LOCK されているため変更しませんでした。変えたい場合は LOCK を解除するよう指示してください。", label, strings.Join(names, " / ")), false, nil
	}
	instruction := op.Instruction
	if utf8.RuneCountInString(instruction) > maxInstructionRunes {
		instruction = string([]rune(instruction)[:maxInstructionRunes])
	}
	partial := op.Type == commandinterpreter.OpPartialRegenerate
	var err error
	switch {
	case partial:
		err = s.startPartial(ctx, ref, op, instruction)
	case s.orch == nil:
		return "", false, ErrUnavailable
	default:
		_, err = s.orch.StartDirected(ctx, ref.Shot.ID, instruction, op.Preserve)
	}
	if err != nil {
		if errors.Is(err, errNoTakeToFix) {
			return label + " には再生成の元にできる Take がまだありません。先に Shot を生成してください。", false, nil
		}
		if errors.Is(err, repair.ErrSpliceUnavailable) {
			return label + " の時間範囲だけの再生成には ffmpeg が必要ですが、見つかりませんでした。", false, nil
		}
		err = mapGenerationError(err)
		switch {
		case errors.Is(err, ErrBusy):
			return label + " は生成中のため、終わってからもう一度指示してください。", false, nil
		case errors.Is(err, ErrBudgetExceeded):
			return label + " は予算上限を超えるため再生成できません。", false, nil
		}
		return "", false, err
	}
	text := label
	if partial {
		text += " の " + domain.FormatTimeRanges(op.Ranges) + " だけを再生成して、元の Take につなぎ込みます"
	} else {
		text += " だけを再生成します"
	}
	if len(op.Changes) > 0 {
		text += "（変更: " + partLabels(op.Changes)
		if len(op.Preserve) > 0 {
			text += " / 保持: " + partLabels(op.Preserve)
		}
		text += "）"
	}
	return text + "。", true, nil
}

// errNoTakeToFix means the Shot has no Take a Partial Regeneration could fix.
var errNoTakeToFix = errors.New("service: the shot has no take to regenerate from")

// startPartial queues the Partial Regeneration of op for the Take the Shot uses: the one the user
// selected, else the AI Pick, else the newest one that is not rejected.
func (s *DirectorService) startPartial(ctx context.Context, ref *repository.ShotRef, op commandinterpreter.Operation, instruction string) error {
	if s.repair == nil {
		return ErrUnavailable
	}
	takes, err := s.takes.ShotTakes(ctx, ref.Shot.ID)
	if err != nil {
		return err
	}
	base, ok := domain.TakeToRepair(takes)
	if !ok {
		return errNoTakeToFix
	}
	_, err = s.repair.StartPartial(ctx, base, repair.PartialRequest{Ranges: op.Ranges, Instruction: instruction, Preserve: op.Preserve})
	return err
}

func partLabels(parts []domain.SpecPart) string {
	labels := make([]string, len(parts))
	for i, p := range parts {
		labels[i] = p.Label()
	}
	return strings.Join(labels, ", ")
}
