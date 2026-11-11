package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
)

// typedIntoCard is what the thread records for a secret typed into a card: the value never is.
const typedIntoCard = "の API キーを入力しました（値はチャットには残しません）"

// pastedNotice is what the chat says to a message that carries what looks like an API key.
const pastedNotice = "API キーらしき文字列が含まれていたため、伏せて記録しました（この発言は読み取っていません）。キーはチャットに書かず、「OpenAI の API キーを設定したい」のように送ってください。キーを入力する欄のあるカードを出します。"

// SubmitSecret stores the secret the user typed into the masked field of a card (POST /projects/:id/messages/:messageId/secret): the card is the newest of its operation
// and not answered, the handler of the operation stores the value (chatops.SecretSubmitter) and the card is answered with a line that never holds the value. Nothing but the
// handler sees the value: it is not an operation, a message, a log line or an event. A refusal (an empty key, one that is too long) leaves the card open to be typed again
// (ValidationError, field "secret").
func (s *ChatService) SubmitSecret(ctx context.Context, projectID, messageID, secret string) (Workspace, error) {
	project, err := s.getProject(ctx, projectID)
	if err != nil {
		return Workspace{}, err
	}
	card, err := s.chat.Get(ctx, projectID, messageID)
	if errors.Is(err, repository.ErrNotFound) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	body := domain.DecodeCardBody(card.Content)
	if card.Kind != domain.KindQuestion || card.RefType != domain.RefOperation || body.Secret == nil {
		return Workspace{}, invalid("secret", "このカードには入力欄がありません")
	}
	submitter, ok := s.secretSubmitter(card.RefID)
	if !ok {
		return Workspace{}, ErrUnavailable
	}
	if _, busy := s.answering.LoadOrStore(card.ID, struct{}{}); busy {
		return Workspace{}, ErrConflict
	}
	defer s.answering.Delete(card.ID)
	if err := s.openOperationCard(ctx, projectID, card); err != nil {
		return Workspace{}, err
	}
	res, err := submitter.Submit(ctx, project, *body.Secret, secret)
	var ref *chatops.Refusal
	if errors.As(err, &ref) {
		return Workspace{}, invalid("secret", "%s", ref.Message)
	}
	if err != nil {
		return Workspace{}, err
	}
	reply := userText("「"+body.Secret.Label+"」"+typedIntoCard, card.ID, nil)
	turn := repository.Turn{ProjectID: projectID, Messages: assignIDs(append([]domain.ChatMessage{reply, assistantText(res.Text)}, res.Messages...))}
	// the secret is stored: the reply that closes the card is recorded whatever happened to ctx
	if err := s.commit(context.WithoutCancel(ctx), turn); err != nil {
		return Workspace{}, err
	}
	return s.workspaceOf(ctx, projectID, turn.Messages)
}

func (s *ChatService) secretSubmitter(operation string) (chatops.SecretSubmitter, bool) {
	h, ok := s.director.chatOps.Handler(commandinterpreter.OpType(operation))
	if !ok {
		return nil, false
	}
	sub, ok := h.(chatops.SecretSubmitter)
	return sub, ok
}

// keyCardOp is the operation of the key card (apikey.OpSetCredential: that package imports this one).
const keyCardOp = "set_credential"

// secretRefusal is what is recorded for a message that carries what looks like an API key: the message with the key hidden, and where to put the key. The message does not reply to a
// card (that would answer the key card, which has to stay open for the key to be typed). It is not read any further: the Brief extractor and the Interpreter (LLMs) never see it.
func secretRefusal(hidden string) []domain.ChatMessage {
	return assignIDs([]domain.ChatMessage{userText(strings.TrimSpace(hidden), "", nil), assistantText(pastedNotice)})
}

// guardSecret records a message that carries what looks like an API key with the key hidden (done is true), whatever mode the Project is in. Besides what secretguard finds, it
// is a key when the card that waits for one is open and the message is a reply to it that is no decline, or one word of the shape of a key nobody knows.
func (s *ChatService) guardSecret(ctx context.Context, project domain.Project, target *domain.ChatMessage, content string) (ws Workspace, done bool, err error) {
	hidden, found := secretguard.Redact(content)
	if !found {
		cardID := ""
		if target != nil && target.RefType == domain.RefOperation && target.RefID == keyCardOp {
			cardID = target.ID
		} else if target == nil && secretguard.OneToken(content) {
			latest, err := s.genApprovals.LatestOperationCards(ctx, project.ID)
			if err != nil {
				return Workspace{}, false, err
			}
			cardID = latest[keyCardOp]
		}
		if cardID == "" || content == confirmNo || chatops.IsDecline(content) {
			return Workspace{}, false, nil
		}
		if replied, err := s.chat.RepliedMessageIDs(ctx, project.ID); err != nil || replied[cardID] {
			return Workspace{}, false, err
		}
		hidden = secretguard.Hidden
	}
	turn := repository.Turn{ProjectID: project.ID, Messages: secretRefusal(hidden)}
	if err := s.commit(ctx, turn); err != nil {
		return Workspace{}, false, err
	}
	ws, err = s.workspaceOf(ctx, project.ID, turn.Messages)
	return ws, true, err
}
