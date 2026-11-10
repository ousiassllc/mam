package service

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/agent/briefing/budget"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// answerCard handles a reply to a card whose answer carries something out: a Continue card (the next Shot) or a confirmation card (a setting). done is false when the
// reply is for neither: the message is read as any other, and target is then what it replies to as far as the Brief goes (a reply to a confirmation card that is neither a
// yes nor a no is a new message: it still replies to the card, which closes it).
func (s *ChatService) answerCard(ctx context.Context, project domain.Project, target *domain.ChatMessage, content string, hasFiles bool) (ws Workspace, next *domain.ChatMessage, done bool, err error) {
	if target == nil || target.Kind != domain.KindQuestion {
		return Workspace{}, target, false, nil
	}
	switch target.RefType {
	case domain.RefContinue:
		ws, err = s.answerContinue(ctx, project, *target, content, hasFiles)
		return ws, target, true, err
	case domain.RefOperation:
		if hasFiles {
			return Workspace{}, target, false, nil
		}
		ws, answered, err := s.answerOperation(ctx, project, *target, content)
		if answered || err != nil {
			return ws, target, true, err
		}
		return Workspace{}, nil, false, nil
	}
	return Workspace{}, target, false, nil
}

// declinedOperation is what the chat says when the user declines a confirmation card: nothing was changed.
const declinedOperation = "やめました。何も変更していません。"

// answerOperation handles a reply to a confirmation card (domain.RefOperation): a button, or its words ("はい", "やめる"). The operation of the card is carried out
// first and the reply, which closes the card, is recorded after it, as for a Continue card: an operation that fails leaves the card open to be answered again. A
// card takes one answer at a time, and only the newest card of an operation can be answered. answered is false when the reply is neither a yes nor a no ("$30 にして"):
// it is then a new message of the user (it still replies to the card, which closes it), and the caller reads it as one.
func (s *ChatService) answerOperation(ctx context.Context, project domain.Project, card domain.ChatMessage, content string) (ws Workspace, answered bool, err error) {
	body := domain.DecodeCardBody(card.Content)
	var (
		action   json.RawMessage
		accepted bool
	)
	declined := content == confirmNo || chatops.IsDecline(content)
	switch {
	case declined:
		action = chatops.DeclineAction(body) // what the card has to clean up when it is declined (a file that waits); nothing for most
	case briefing.IsConfirmation(content) && len(body.OptionActions) > 0 && len(body.OptionActions[0]) > 0 && len(body.Options) > 0 && body.Options[0] == confirmYes:
		action, accepted = body.OptionActions[0], true
	default:
		if action, accepted = chatops.ActionOf(body, content); !accepted {
			return Workspace{}, false, nil
		}
	}
	if _, busy := s.answering.LoadOrStore(card.ID, struct{}{}); busy {
		return Workspace{}, true, ErrConflict
	}
	defer s.answering.Delete(card.ID)
	if err := s.openOperationCard(ctx, project.ID, card); err != nil {
		return Workspace{}, true, err
	}
	result := actionResult{text: declinedOperation}
	switch {
	case accepted:
		if result, err = s.director.applyAction(ctx, project, action); err != nil {
			return Workspace{}, true, err
		}
	case len(action) > 0:
		if done, err := s.director.applyAction(ctx, project, action); err != nil {
			s.logger.Error("clean up a declined confirmation card failed", "project_id", project.ID, "message_id", card.ID, "err", err) // the answer is still "declined"
		} else if done.text != "" {
			result.text = done.text
		}
	}
	turn := repository.Turn{ProjectID: project.ID, Messages: assignIDs(append([]domain.ChatMessage{userText(content, card.ID, nil), assistantText(result.text)}, result.messages...))}
	// the operation was carried out, so the reply that closes the card is recorded whatever happened to ctx
	if err := s.commit(context.WithoutCancel(ctx), turn); err != nil {
		return Workspace{}, true, err
	}
	s.kick(ctx, project.ID) // the Orchestrator waited for the answer (a budget that was not confirmed): any answer closes the card, a decline too, and the generation goes on
	ws, err = s.workspaceOf(ctx, project.ID, turn.Messages)
	return ws, true, err
}

// openOperationCard is nil when the card can still be answered: nobody replied to it and no newer card of the same operation replaced it.
func (s *ChatService) openOperationCard(ctx context.Context, projectID string, card domain.ChatMessage) error {
	replied, err := s.chat.RepliedMessageIDs(ctx, projectID)
	if err != nil {
		return err
	}
	if replied[card.ID] {
		return ErrConflict
	}
	latest, err := s.genApprovals.LatestOperationCards(ctx, projectID)
	if err != nil {
		return err
	}
	if latest[card.RefID] != card.ID {
		return ErrConflict
	}
	return nil
}

// answerEscalation carries out the operation of the button a reply to an Escalation picks (CardBody.OptionActions), and returns what to tell; picked is false when
// the reply is none of the buttons (or they carry no operation), and the Escalation is only noted, as before.
func (s *ChatService) answerEscalation(ctx context.Context, project domain.Project, card domain.ChatMessage, reply string) (text string, picked bool, err error) {
	action, ok := chatops.ActionOf(domain.DecodeCardBody(card.Content), reply)
	if !ok {
		return "", false, nil
	}
	done, err := s.director.applyAction(ctx, project, action)
	if done.failed {
		return done.text, true, nil // the button was picked but its operation could not run: say only that, never "executed"
	}
	return "判断を受け付けて、実行しました。\n" + done.text, err == nil, err
}

// statedBudgetCards is the confirmation card for the budget a free message states ("予算は $20 で", #1139): what `budget.Stated` reads from the words (the Brief's extraction of a stated budget). The budget is not set
// before the user answers the card; nothing when the message states none.
func (s *core) statedBudgetCards(ctx context.Context, project domain.Project, content string) ([]domain.ChatMessage, error) {
	stated := budget.Stated(content)
	if !stated.Any() && !stated.Yen {
		return nil, nil
	}
	return s.director.budgetCards(ctx, project, stated)
}
