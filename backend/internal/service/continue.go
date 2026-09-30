package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/continuation"
	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// ContinueService makes the next Shot or Scene after a completed Shot (F-16, UC-4): it analyses
// the Shot and offers NEXT candidates as a Question card, then, once the user has chosen (or left
// it to the AI), adds the Shot to the Project and generates it from the World State.
type ContinueService struct {
	*core
	generation *GenerationService
	shots      *repository.GenerationRepository
	agent      *continuation.Agent // nil when the Reasoning LLM is not configured
}

// ConfirmInput is the choice of a Continue card. Instruction is the chosen candidate or the
// user's own direction; Delegate leaves the choice to the AI (Instruction is then ignored).
type ConfirmInput struct {
	Instruction string
	Delegate    bool
}

// check verifies that Continue can start from the Shot: generation is configured, the Shot is not
// being generated and it has a Take that was not rejected.
func (s *ContinueService) check(ctx context.Context, shotID string) (repository.ShotRef, error) {
	if s.agent == nil || s.generation.gen == nil {
		return repository.ShotRef{}, ErrUnavailable
	}
	ref, err := s.shots.GetShot(ctx, shotID)
	if err != nil {
		return repository.ShotRef{}, notFound(err)
	}
	if ref.Shot.State == domain.ShotGenerating {
		return repository.ShotRef{}, ErrBusy
	}
	takes, err := s.plans.ListTakes(ctx, ref.Scene.ID)
	if err != nil {
		return repository.ShotRef{}, err
	}
	for _, t := range takes[shotID] {
		if t.Status != domain.TakeRejected {
			return ref, nil
		}
	}
	return repository.ShotRef{}, ErrConflict // nothing has been completed to continue from
}

// input gathers what the Continue Agent reads: the Shot, its spec, the World State after it and
// the story so far.
func (s *ContinueService) input(ctx context.Context, ref repository.ShotRef) (continuation.Input, error) {
	project, err := s.projects.Get(ctx, ref.ProjectID)
	if err != nil {
		return continuation.Input{}, notFound(err)
	}
	brief, err := s.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return continuation.Input{}, notFound(err)
	}
	world, err := s.shots.WorldContext(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return continuation.Input{}, err
	}
	shots, err := s.shots.StoryShots(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return continuation.Input{}, err
	}
	in := continuation.Input{Brief: brief, Template: project.ProductionTemplate, Scene: ref.Scene, Shot: ref.Shot, World: world}
	if spec, ok := ref.Shot.CanonicalSpec(); ok {
		in.Spec = &spec
	}
	for _, sh := range shots {
		in.Story = append(in.Story, fmt.Sprintf("Scene %d / Shot %d: %s", sh.Scene.OrderIndex+1, sh.Shot.OrderIndex+1, sh.Shot.Objective))
	}
	return in, nil
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
	direction := strings.TrimSpace(in.Instruction)
	if in.Delegate {
		direction = ""
	} else if direction == "" {
		return ShotFragment{}, invalid("instruction", "続きの展開を選ぶか、指示を入力してください")
	}
	if utf8.RuneCountInString(direction) > maxInstructionRunes {
		return ShotFragment{}, invalid("instruction", "%d 文字以内で入力してください", maxInstructionRunes)
	}
	ref, err := s.check(ctx, shotID)
	if err != nil {
		return ShotFragment{}, err
	}
	agentIn, err := s.input(ctx, ref)
	if err != nil {
		return ShotFragment{}, err
	}
	plan, err := s.agent.Plan(ctx, agentIn, direction)
	if err != nil {
		return ShotFragment{}, aiError(err)
	}
	var newScene *domain.Scene
	shot := domain.Shot{SceneID: ref.Shot.SceneID, Objective: plan.Shot.Objective, Duration: plan.Shot.DurationSeconds}
	if plan.NewScene {
		newScene = &domain.Scene{BranchID: ref.Scene.BranchID, Title: plan.Scene.Title, Location: plan.Scene.Location, Time: plan.Scene.Time, Objective: plan.Scene.Objective}
	}
	shot, err = s.plans.AppendContinuation(ctx, ref.ProjectID, newScene, shot)
	if err != nil {
		return ShotFragment{}, notFound(err)
	}
	scene, err := s.plans.GetSceneByID(ctx, shot.SceneID)
	if err != nil {
		return ShotFragment{}, notFound(err)
	}
	chat := &ChatService{s.core}
	note := fmt.Sprintf("続きを追加しました: Scene %d「%s」 Shot %d: %s", scene.OrderIndex+1, scene.Title, shot.OrderIndex+1, shot.Objective)
	if err := chat.Announce(ctx, ref.ProjectID, assistantText(note)); err != nil {
		s.logger.Error("record continuation in the thread failed", "project_id", ref.ProjectID, "err", err)
	}
	if _, err := s.generation.gen.Start(ctx, shot.ID, direction); err != nil {
		return ShotFragment{}, mapGenerationError(err)
	}
	// the user asked for this Shot (by choosing or confirming the direction): a generate, as if
	// they had pressed Generate on it (spec/spec.md §28.3)
	s.recordIntervention(ctx, domain.Intervention{ShotID: shot.ID, Kind: domain.InterventionGenerate})
	return s.generation.Shot(ctx, shot.ID)
}

// aiError reports a failed Continue Agent call with the reason the user can act on.
func aiError(err error) error { return &AIError{Reason: generation.FailureReason(err), Err: err} }

// answerContinue handles a reply to a Continue card (an option button, "AIに任せる", or the user's
// own words). The reply is recorded first, which closes the card, and then the next Shot is made;
// when that fails the error is returned and the user asks for a new card. It is called by
// PostMessage before anything is written.
func (s *ChatService) answerContinue(ctx context.Context, project domain.Project, card domain.ChatMessage, content string, hasAttachments bool) (Workspace, error) {
	if hasAttachments {
		return Workspace{}, invalid("attachment_asset_ids", "この質問には添付できません")
	}
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
	if err := s.commit(ctx, turn); err != nil {
		return Workspace{}, err
	}
	if _, err := s.continuer.Confirm(ctx, card.RefID, choice); err != nil {
		return Workspace{}, err
	}
	return s.workspaceOf(ctx, project.ID, turn.Messages)
}
