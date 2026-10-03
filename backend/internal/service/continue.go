package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/continuation"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ContinueService makes the next Shot or Scene after a completed Shot (F-16, UC-4): it analyses
// the Shot and offers NEXT candidates as a Question card, then, once the user has chosen (or left
// it to the AI), adds the Shot to the Project and generates it from the World State.
type ContinueService struct {
	*core
	generation *GenerationService
	shots      *repository.GenerationRepository
	// brands are the Safety / Brand Constraints the user's direction is checked against before the
	// Plan is paid for or a Shot is added.
	brands *repository.BrandRepository
	agent  *continuation.Agent // nil when the Reasoning LLM is not configured
	// tools takes the final frame of the Take the next Shot continues from; nil without ffmpeg (the
	// Agent then reads the States from the text only).
	tools *media.Tools
}

// ConfirmInput is the choice of a Continue card. Instruction is the chosen candidate or the
// user's own direction; Delegate leaves the choice to the AI (Instruction is then ignored).
type ConfirmInput struct {
	Instruction string
	Delegate    bool
}

// check verifies that Continue can start from the Shot: generation is configured, the Shot is not
// being generated or repaired and it has a Take that was not rejected.
func (s *ContinueService) check(ctx context.Context, shotID string) (repository.ShotRef, error) {
	if s.agent == nil || s.generation.gen == nil {
		return repository.ShotRef{}, ErrUnavailable
	}
	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return repository.ShotRef{}, notFound(err)
	}
	if ref.Shot.State.Busy() {
		return repository.ShotRef{}, ErrBusy
	}
	takes, err := s.plans.ListTakes(ctx, ref.Scene.ID)
	if err != nil {
		return repository.ShotRef{}, err
	}
	if !domain.CanContinue(ref.Shot.State, takes[shotID]) {
		return repository.ShotRef{}, ErrConflict // nothing has been completed to continue from
	}
	return ref, nil
}

// input gathers what the Continue Agent reads: the Shot, its spec, the World State after it and
// the story so far.
func (s *ContinueService) input(ctx context.Context, ref repository.ShotRef) (continuation.Input, error) {
	in, story, err := s.gather(ctx, ref, ref.Scene.BranchID)
	if err != nil {
		return continuation.Input{}, err
	}
	in.Story = storyLines(story)
	s.fromShot(ctx, &in, ref)
	return in, nil
}

// branchInput gathers what the Continue Agent reads to plan the first Shot of a Branch made at ref
// (typ) from the series (trunk or Branch) series: the Shot it continues from is the last Shot at or
// before the Branch Point, so for a Branch at a Scene it is the last Shot before that Scene (none
// when nothing precedes it: NoShot) and not the clicked Shot, which lies after the fork and is not
// part of the Branch's story. The story is cut at the Branch Point the same way.
func (s *ContinueService) branchInput(ctx context.Context, ref repository.ShotRef, typ domain.BranchPointType, series string) (continuation.Input, error) {
	in, story, err := s.gather(ctx, ref, series)
	if err != nil {
		return continuation.Input{}, err
	}
	story = repository.CutAtBranchPoint(story, typ, ref.Scene.ID, ref.Shot.ID)
	in.Story = storyLines(story)
	from := ref
	if typ == domain.BranchAtScene {
		if len(story) == 0 {
			in.NoShot = true
			return in, nil
		}
		from = story[len(story)-1]
		in.Scene = from.Scene
	}
	s.fromShot(ctx, &in, from)
	return in, nil
}

// gather reads what does not depend on the Shot: the Brief, the World State and the story of series.
func (s *ContinueService) gather(ctx context.Context, ref repository.ShotRef, series string) (continuation.Input, []repository.ShotRef, error) {
	project, err := s.projects.Get(ctx, ref.ProjectID)
	if err != nil {
		return continuation.Input{}, nil, notFound(err)
	}
	brief, err := s.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return continuation.Input{}, nil, notFound(err)
	}
	world, err := s.shots.WorldContext(ctx, ref.ProjectID, series)
	if err != nil {
		return continuation.Input{}, nil, err
	}
	shots, err := s.shots.StoryShots(ctx, ref.ProjectID, series)
	if err != nil {
		return continuation.Input{}, nil, err
	}
	return continuation.Input{Brief: brief, Template: project.ProductionTemplate, Scene: ref.Scene, World: world}, shots, nil
}

// fromShot sets the Shot the next one continues from: the Shot, its spec and its final frame.
func (s *ContinueService) fromShot(ctx context.Context, in *continuation.Input, ref repository.ShotRef) {
	in.Shot = ref.Shot
	if spec, ok := ref.Shot.CanonicalSpec(); ok {
		in.Spec = &spec
	}
	in.FinalFrame = s.finalFrame(ctx, ref)
}

// storyLines are the Shots as the "story so far" of the Continue Agent's prompt.
func storyLines(shots []repository.ShotRef) []string {
	lines := make([]string, 0, len(shots))
	for _, sh := range shots {
		lines = append(lines, fmt.Sprintf("Scene %d / Shot %d: %s", sh.Scene.OrderIndex+1, sh.Shot.OrderIndex+1, sh.Shot.Objective))
	}
	return lines
}

// finalFrame returns the last frame of the Take the next Shot continues from (the selected Take,
// else the AI pick, else the newest one that was not rejected), or nil when there is none: no
// ffmpeg, no video file, or ffmpeg failed. It never fails Continue: the Agent then works from text.
func (s *ContinueService) finalFrame(ctx context.Context, ref repository.ShotRef) *llm.Image {
	if s.tools == nil || s.assetsDir == "" {
		return nil
	}
	takes, err := s.plans.ListTakes(ctx, ref.Scene.ID)
	if err != nil {
		s.logger.Error("list takes for the final frame failed", "shot_id", ref.Shot.ID, "err", err)
		return nil
	}
	var from *domain.Take
	rank := func(t domain.Take) int {
		switch t.Status {
		case domain.TakeSelected:
			return 3
		case domain.TakeAIPick:
			return 2
		case domain.TakeRejected:
			return 0
		}
		return 1
	}
	list := takes[ref.Shot.ID]
	for i, t := range list {
		if t.FilePath == "" || rank(t) == 0 {
			continue
		}
		if from == nil || rank(t) >= rank(*from) {
			from = &list[i]
		}
	}
	if from == nil {
		return nil
	}
	src := filepath.Join(s.assetsDir, filepath.FromSlash(from.FilePath))
	info, err := s.tools.Probe(ctx, src)
	if err != nil {
		s.logger.Error("probe the take for the final frame failed", "take_id", from.ID, "err", err)
		return nil
	}
	dir, err := os.MkdirTemp("", "mam-final-frame-")
	if err != nil {
		s.logger.Error("create a directory for the final frame failed", "err", err)
		return nil
	}
	defer func() { _ = os.RemoveAll(dir) }()
	dst := filepath.Join(dir, "final.jpg")
	if err := s.tools.Thumbnail(ctx, src, dst, info.Duration-0.1); err != nil {
		s.logger.Error("take the final frame failed", "take_id", from.ID, "err", err)
		return nil
	}
	data, err := os.ReadFile(dst) // #nosec G304 -- a file this function just wrote
	if err != nil {
		s.logger.Error("read the final frame failed", "take_id", from.ID, "err", err)
		return nil
	}
	return &llm.Image{MimeType: "image/jpeg", Data: data}
}

// Propose analyses the completed Shot and posts the Continue card: the analysed States and the
// NEXT candidates A/B/C as options, plus "AIに任せる". A free direction is a reply to the card. It
// returns the card as the thread shows it.
func (s *ContinueService) Propose(ctx context.Context, shotID string) (domain.Project, domain.ThreadMessage, error) {
	ref, err := s.check(ctx, shotID)
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	in, err := s.input(ctx, ref)
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	proposal, err := s.agent.Propose(ctx, in)
	if err != nil {
		return domain.Project{}, domain.ThreadMessage{}, aiError(err)
	}
	card := domain.ChatMessage{
		ID: domain.NewID(), Role: domain.RoleAssistant, Kind: domain.KindQuestion, RefType: domain.RefContinue, RefID: shotID,
		Content: domain.EncodeCardBody(cardBody(ref.Shot, proposal)),
	}
	chat := &ChatService{s.core}
	if err := chat.Announce(ctx, ref.ProjectID, card); err != nil {
		return domain.Project{}, domain.ThreadMessage{}, err
	}
	return chat.Card(ctx, ref.ProjectID, card.ID)
}

// candidateLabels are the letters of the NEXT candidates. D (free direction) and E (leave it to
// the AI) follow them.
const candidateLabels = "ABCDE"

func cardBody(shot domain.Shot, p continuation.Proposal) domain.CardBody {
	var text strings.Builder
	fmt.Fprintf(&text, "Shot %d の続きを作れます。最終フレーム時点の状態:\n", shot.OrderIndex+1)
	for _, r := range p.Analysis.Rows() {
		fmt.Fprintf(&text, "・%s: %s\n", r.Label, r.Value)
	}
	next := len(p.Candidates)
	fmt.Fprintf(&text, "\n次の展開を選んでください。%c は入力欄から自由に指示、%c は「%s」です。", candidateLabels[next], candidateLabels[next+1], domain.DelegateOption)
	options := make([]string, next)
	for i, c := range p.Candidates {
		options[i] = candidateOption(i, c)
	}
	return domain.CardBody{Text: text.String(), Options: options}
}

func candidateOption(i int, text string) string { return string(candidateLabels[i]) + ". " + text }

// directionOf is the direction a reply to a Continue card stands for: the candidate text when the
// reply is one of the card's options, "AIに任せる" as delegation, otherwise the user's own words.
func directionOf(card domain.CardBody, reply string) ConfirmInput {
	reply = strings.TrimSpace(reply)
	if reply == domain.DelegateOption {
		return ConfirmInput{Delegate: true}
	}
	for i := range card.Options {
		if reply == card.Options[i] {
			return ConfirmInput{Instruction: strings.TrimPrefix(reply, string(candidateLabels[i])+". ")}
		}
	}
	return ConfirmInput{Instruction: reply}
}

// Confirm adds the next Shot after the completed one (in a new Scene when the AI says the story
// moves on) and starts generating it. The Shot is generated from the World State the completed
// Shots left, so characters, products, camera and story carry over. It returns the new Shot in
// the GENERATING state. A generation that cannot start (budget) leaves the new Shot in the Project,
// ready to be generated later.
func (s *ContinueService) Confirm(ctx context.Context, shotID string, in ConfirmInput) (ShotFragment, error) {
	frag, _, err := s.confirm(ctx, shotID, in)
	return frag, err
}

// confirm is Confirm that also reports whether the new Shot was added to the Project: after that
// the continuation cannot be asked for again without making a second Shot, even if starting its
// generation failed.
func (s *ContinueService) confirm(ctx context.Context, shotID string, in ConfirmInput) (frag ShotFragment, added bool, err error) {
	direction := strings.TrimSpace(in.Instruction)
	if in.Delegate {
		direction = ""
	} else if direction == "" {
		return ShotFragment{}, false, invalid("instruction", "続きの展開を選ぶか、指示を入力してください")
	}
	if utf8.RuneCountInString(direction) > maxInstructionRunes {
		return ShotFragment{}, false, invalid("instruction", "%d 文字以内で入力してください", maxInstructionRunes)
	}
	ref, err := s.check(ctx, shotID)
	if err != nil {
		return ShotFragment{}, false, err
	}
	if err := checkBrandTexts(ctx, s.brands, ref.ProjectID, direction); err != nil {
		return ShotFragment{}, false, err // a forbidden expression: no Plan, no Shot, the card stays open
	}
	agentIn, err := s.input(ctx, ref)
	if err != nil {
		return ShotFragment{}, false, err
	}
	plan, err := s.agent.Plan(ctx, agentIn, direction)
	if err != nil {
		return ShotFragment{}, false, aiError(err)
	}
	var newScene *domain.Scene
	shot := domain.Shot{SceneID: ref.Shot.SceneID, Objective: plan.Shot.Objective, Duration: plan.Shot.DurationSeconds}
	if plan.NewScene {
		newScene = &domain.Scene{BranchID: ref.Scene.BranchID, Title: plan.Scene.Title, Location: plan.Scene.Location, Time: plan.Scene.Time, Objective: plan.Scene.Objective}
	}
	shot, err = s.plans.AppendContinuation(ctx, ref.ProjectID, newScene, shot)
	if err != nil {
		return ShotFragment{}, false, notFound(err)
	}
	scene, err := s.plans.GetSceneByID(ctx, shot.SceneID)
	if err != nil {
		return ShotFragment{}, true, notFound(err)
	}
	// the Shot is in the Project: what follows is recorded even if the client goes away, or the
	// card would stay open and a second answer would make a second Shot (and a paid generation)
	done := context.WithoutCancel(ctx)
	chat := &ChatService{s.core}
	note := fmt.Sprintf("続きを追加しました: Scene %d「%s」 Shot %d: %s", scene.OrderIndex+1, scene.Title, shot.OrderIndex+1, shot.Objective)
	if err := chat.Announce(done, ref.ProjectID, assistantText(note)); err != nil {
		s.logger.Error("record continuation in the thread failed", "project_id", ref.ProjectID, "err", err)
	}
	if _, err := s.generation.gen.StartByPolicy(ctx, shot.ID, direction, nil); err != nil {
		return ShotFragment{}, true, mapGenerationError(err)
	}
	// the user asked for this Shot (by choosing or confirming the direction): a generate, as if
	// they had pressed Generate on it (spec/spec.md §28.3)
	s.recordIntervention(done, domain.Intervention{ShotID: shot.ID, Kind: domain.InterventionGenerate})
	frag, err = s.generation.Shot(ctx, shot.ID)
	return frag, true, err
}

// aiError reports a failed Continue Agent call with the reason the user can act on.
func aiError(err error) error { return &AIError{Reason: generation.FailureReason(err), Err: err} }

// answerContinue handles a reply to a Continue card (an option button, "AIに任せる", or the user's
// own words). The next Shot is made first and the reply, which closes the card, is recorded after
// it: a Confirm that fails before the Shot is added (Plan, AI error) leaves the card open to be
// answered again. When the Shot was added but its generation could not start (budget), the card
// closes as well, so that a second answer does not make a second Shot. The reply gets its id before
// Confirm, so it stays ahead of the notes Confirm writes in the thread. It is called by
// PostMessage before anything is written. A card answers one reply at a time: the reply that
// arrives while another one to the same card is still being handled is a conflict (the closing reply
// is only recorded after Confirm, so the thread alone cannot tell).
func (s *ChatService) answerContinue(ctx context.Context, project domain.Project, card domain.ChatMessage, content string, hasAttachments bool) (Workspace, error) {
	if hasAttachments {
		return Workspace{}, invalid("attachment_asset_ids", "この質問には添付できません")
	}
	if _, busy := s.answering.LoadOrStore(card.ID, struct{}{}); busy {
		return Workspace{}, ErrConflict
	}
	defer s.answering.Delete(card.ID)
	replied, err := s.chat.RepliedMessageIDs(ctx, project.ID)
	if err != nil {
		return Workspace{}, err
	}
	if replied[card.ID] {
		return Workspace{}, ErrConflict
	}
	if _, err := s.continuer.check(ctx, card.RefID); err != nil {
		return Workspace{}, err
	}
	choice := directionOf(domain.DecodeCardBody(card.Content), content)
	turn := repository.Turn{ProjectID: project.ID, Messages: assignIDs([]domain.ChatMessage{userText(content, card.ID, nil)})}
	_, added, confirmErr := s.continuer.confirm(ctx, card.RefID, choice)
	if confirmErr != nil && !added {
		return Workspace{}, confirmErr
	}
	// the Shot was added, so the reply that closes the card is recorded whatever happened to ctx
	if err := s.commit(context.WithoutCancel(ctx), turn); err != nil {
		return Workspace{}, err
	}
	if confirmErr != nil {
		return Workspace{}, confirmErr
	}
	return s.workspaceOf(ctx, project.ID, turn.Messages)
}
