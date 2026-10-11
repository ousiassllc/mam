package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

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
// can act on are reported in the message; an error that is returned (a Brand violation, an
// unexpected failure) has already been answered in the thread (reportCommandFailure), so that the
// thread does not end with the user's words alone.
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
	if ref != nil && ref.Scene.BranchID != "" {
		// The Shot is on a Branch: the numbers of the message and of the Interpreter's answer are those
		// of that Branch's story, not of the trunk (whose Scene 1 / Shot 1 is another Shot).
		if shots, err = s.gens.StoryShots(ctx, project.ID, ref.Scene.BranchID); err != nil {
			return domain.ChatMessage{}, err
		}
	}
	cmd, err := s.interpret(ctx, content, shots, ref)
	if err != nil {
		if !isInterpreterAIError(err) {
			// A failure of ours (the repository), not of the model: it is an error, logged and answered
			// in the thread like any other failure of a direction, never a "success" with a message.
			s.logger.Error("interpret director message failed", "project_id", project.ID, "err", err)
			s.reportCommandFailure(ctx, project.ID, outcome{}, err)
			return domain.ChatMessage{}, err
		}
		return assistantText("指示を読み取れませんでした: " + commandFailureReason(err)), nil
	}
	if project.Mode != domain.ModeDirector {
		// Outside DIRECTOR MODE only a request to remember something is carried out: AUTO MODE does
		// not take Shot directions from the chat.
		return s.rememberOnly(ctx, project, cmd)
	}
	if cmd.Question != "" {
		return assistantText(cmd.Question), nil
	}
	// Reply to a Take or Keyframe card outranks Scene/Shot numbers in the message. Numbers, then the
	// latest Take card, then the Project's only Shot (both from contextShot) apply only without
	// that reply.
	if target == nil || (target.Kind != domain.KindTake && target.Kind != domain.KindKeyframe) {
		if shot := shotNamed(shots, ref, cmd.Scene, cmd.Shot); shot != nil {
			ref = shot
		}
	}
	if err := s.refuseBrand(ctx, project.ID, content, cmd); err != nil {
		s.reportCommandFailure(ctx, project.ID, outcome{}, err)
		return domain.ChatMessage{}, err
	}
	res, err := s.execute(ctx, project, ref, cmd, content, false)
	if err != nil {
		s.reportCommandFailure(ctx, project.ID, res, err)
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
	res, err := s.execute(ctx, project, nil, cmd, "", false)
	if err != nil {
		s.reportCommandFailure(ctx, project.ID, res, err)
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

// contextShot resolves the Shot a message is about without the words of the message: the Take or
// Keyframe card it replies to, else the latest Take card of the thread, else the only Shot of the
// Project. A reply to a card whose Shot is not known resolves to none (the Director asks), never to
// another Shot.
func (s *DirectorService) contextShot(ctx context.Context, projectID string, target *domain.ChatMessage, shots []repository.ShotRef) (*repository.ShotRef, error) {
	if target != nil && target.Kind == domain.KindKeyframe {
		ref, err := s.gens.ShotOfKeyframe(ctx, projectID, target.RefID)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &ref, nil
	}
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

// shotNamed finds the Shot by the numbers a message names (1-based); nil when it names none. The
// numbers of a Branch's story are not unique (a Branch Scene counts from 1 again), so the Shot the
// context already resolved (target) is the answer when it has those numbers.
func shotNamed(shots []repository.ShotRef, target *repository.ShotRef, scene, shot int) *repository.ShotRef {
	if scene < 1 || shot < 1 {
		return nil
	}
	if target != nil && target.Scene.OrderIndex+1 == scene && target.Shot.OrderIndex+1 == shot {
		return target
	}
	for i := range shots {
		if shots[i].Scene.OrderIndex+1 == scene && shots[i].Shot.OrderIndex+1 == shot {
			return &shots[i]
		}
	}
	return nil
}
