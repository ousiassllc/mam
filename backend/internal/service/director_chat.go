package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/shotpick"
	"github.com/ousiassllc/mam/backend/internal/service/silence"
)

// wants reports whether a chat message is a direction for the Command Interpreter: a message that asks
// the team to remember something (F-31, in any mode), one that asks for an operation of any mode past the Brief (commandinterpreter.AsksAnyMode: a budget, a setting; a budget stated while the Brief is drafted is read by
// statedBudgetCards from every message, #1139), or, in DIRECTOR MODE past the Brief, any message. A message that answers an open card never is.
func (s *DirectorService) wants(project domain.Project, target *domain.ChatMessage, content string) bool {
	if content == "" || (target != nil && isAnswerable(target.Kind)) {
		return false
	}
	if domain.AsksToRemember(content) || (project.State != domain.StateDraft && commandinterpreter.AsksAnyMode(content)) {
		return true
	}
	// While the Brief is drafted every other message answers it: "動画は縦型にして" asks for a vertical video, not for a model assignment, however much it sounds like one (the
	// settings a new app needs first, and what the chat refuses on purpose, are the exceptions); past it the settings of the whole app are asked for in any mode (#1140)
	if project.State == domain.StateDraft {
		return commandinterpreter.AsksSetup(content)
	}
	return commandinterpreter.AsksGlobal(content) || project.Mode == domain.ModeDirector
}

// Chat interprets a message of the thread (already stored) and carries it out. The result is the AI
// messages to append to the thread: what was done, what was refused, or what to ask, and the confirmation cards of the operations that wait for the user. Failures the user
// can act on are reported in the message; an error that is returned (a Brand violation, an
// unexpected failure) has already been answered in the thread (reportCommandFailure), so that the thread does not end with the user's words alone.
func (s *DirectorService) Chat(ctx context.Context, project domain.Project, target *domain.ChatMessage, user domain.ChatMessage) (msgs []domain.ChatMessage, err error) {
	content := user.Content
	defer silence.Recover(s.logger, project.ID, user.ID, func(err error) error { return s.reportCommandFailure(ctx, project.ID, user.ID, outcome{}, err) }, &err)
	if reply, refused := commandinterpreter.Refused(content); refused {
		return []domain.ChatMessage{assistantText(reply)}, nil
	}
	if s.interpreter == nil {
		// what the message plainly asks for is still done without the model: the API key of the LLM is the first thing to set (#1140)
		if ops := commandinterpreter.Detected(content); len(ops) > 0 {
			return s.anyModeOnly(ctx, project, user.ID, commandinterpreter.Command{Operations: ops})
		}
		return []domain.ChatMessage{assistantText("LLM が設定されていないため、指示を解釈できません（設定画面で API キーを登録してください。「OpenAI の API キーを設定したい」のように送ると、ここでキーを入力する欄を出します）。")}, nil
	}
	shots, err := s.gens.ListProjectShots(ctx, project.ID)
	if err != nil {
		return nil, s.reportCommandFailure(ctx, project.ID, user.ID, outcome{}, err)
	}
	ref, err := s.contextShot(ctx, project.ID, target, shots)
	if err != nil {
		return nil, s.reportCommandFailure(ctx, project.ID, user.ID, outcome{}, err)
	}
	if ref != nil && ref.Scene.BranchID != "" {
		// The Shot is on a Branch: the numbers of the message and of the Interpreter's answer are those
		// of that Branch's story, not of the trunk (whose Scene 1 / Shot 1 is another Shot).
		if shots, err = s.gens.StoryShots(ctx, project.ID, ref.Scene.BranchID); err != nil {
			return nil, s.reportCommandFailure(ctx, project.ID, user.ID, outcome{}, err)
		}
	}
	cmd, err := s.interpret(ctx, project.ID, content, shots, ref)
	if err != nil {
		if !isInterpreterAIError(err) {
			// A failure of ours (the repository), not of the model: it is an error, logged and answered
			// in the thread like any other failure of a direction, never a "success" with a message.
			return nil, s.reportCommandFailure(ctx, project.ID, user.ID, outcome{}, err)
		}
		return []domain.ChatMessage{assistantText("指示を読み取れませんでした: " + commandFailureReason(err))}, nil
	}
	cmd.Operations = commandinterpreter.WithDetected(cmd.Operations, content)
	if project.Mode != domain.ModeDirector || (cmd.Question != "" && slices.ContainsFunc(cmd.Operations, commandinterpreter.Operation.AnyMode)) {
		// Only the operations of any mode are carried out (a note, the budget, a setting) outside DIRECTOR MODE (AUTO MODE does not take Shot directions from the chat) and when the
		// model asks a question while the message plainly asks for such an operation too (WithDetected: an API key card): the question is told with it, not instead.
		return s.anyModeOnly(ctx, project, user.ID, cmd)
	}
	if cmd.Question != "" {
		return []domain.ChatMessage{assistantText(cmd.Question)}, nil
	}
	// Reply to a Take or Keyframe card outranks Scene/Shot numbers in the message. Numbers, then the
	// latest Take card, then the Project's only Shot (both from contextShot) apply only without
	// that reply.
	if target == nil || (target.Kind != domain.KindTake && target.Kind != domain.KindKeyframe) {
		if shot := shotpick.Named(shots, ref, cmd.Scene, cmd.Shot); shot != nil {
			ref = shot
		}
	}
	if err := s.refuseBrand(ctx, project.ID, content, cmd); err != nil {
		return nil, s.reportCommandFailure(ctx, project.ID, user.ID, outcome{}, err)
	}
	res, err := s.execute(ctx, project, ref, cmd, content, false)
	if err != nil {
		return nil, s.reportCommandFailure(ctx, project.ID, user.ID, res, err)
	}
	return res.messages(), nil
}

// anyModeOnly carries out the operations of a Command that AUTO MODE takes too (create_note, set_budget: commandinterpreter.Spec.AnyMode); AUTO MODE does not take
// Shot directions from the chat.
func (s *DirectorService) anyModeOnly(ctx context.Context, project domain.Project, messageID string, cmd commandinterpreter.Command) ([]domain.ChatMessage, error) {
	cmd.Operations = slices.DeleteFunc(cmd.Operations, func(op commandinterpreter.Operation) bool { return !op.AnyMode() })
	if len(cmd.Operations) == 0 {
		return []domain.ChatMessage{assistantText("メモ・予算・設定として読み取れる内容が見つかりませんでした。覚えておきたい内容や、予算（例:「予算は $20 で」）、変えたい設定（例:「OpenAI の API キーを設定したい」）を、もう一度教えてください。")}, nil
	}
	res, err := s.execute(ctx, project, nil, cmd, "", false)
	if err != nil {
		return nil, s.reportCommandFailure(ctx, project.ID, messageID, res, err)
	}
	return res.messages(), nil
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
