package chatops

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// The buttons of a confirmation card, which the composer answers with the same words.
const (
	Yes = "はい、この内容で進める"
	No  = "やめる"
)

// MaxChoices is how many candidates a card offers as buttons; a name that fits more is asked to be narrowed down (every operation that offers a choice uses this one number).
const MaxChoices = 6

// Body is the body of the confirmation card of op: a yes that carries op (or what the card says it carries: Card.Op), or one button per choice, or the masked field of a
// secret with no yes at all. "やめる" is always there and carries Card.Decline (nothing for most cards).
func Body(op commandinterpreter.Operation, card Card) (domain.CardBody, error) {
	body := domain.CardBody{Text: card.Text}
	switch {
	case card.Secret != nil:
		body.Secret = card.Secret
	case len(card.Choices) > 0:
		for _, c := range card.Choices {
			action, err := json.Marshal(c.Op)
			if err != nil {
				return domain.CardBody{}, err
			}
			body.Options, body.OptionActions = append(body.Options, c.Label), append(body.OptionActions, action)
		}
	default:
		if card.Op != nil {
			op = *card.Op
		}
		action, err := json.Marshal(op)
		if err != nil {
			return domain.CardBody{}, err
		}
		body.Options, body.OptionActions = []string{Yes}, []json.RawMessage{action}
	}
	var decline json.RawMessage
	if card.Decline != nil {
		action, err := json.Marshal(*card.Decline)
		if err != nil {
			return domain.CardBody{}, err
		}
		decline = action
	}
	body.Options, body.OptionActions = append(body.Options, No), append(body.OptionActions, decline)
	return body, nil
}

// Message is the confirmation card of op as the chat posts it (ref_type operation, ref_id the operation type): a handler posts one itself when an answer leads to another
// question (the provider that was just chosen needs a key).
func Message(op commandinterpreter.Operation, card Card) (domain.ChatMessage, error) {
	body, err := Body(op, card)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	return domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindQuestion, RefType: domain.RefOperation, RefID: string(op.Type), Content: domain.EncodeCardBody(body),
	}, nil
}

var declineWords = []string{"やめる", "やめて", "やめます", "やめとく", "いいえ", "いや", "キャンセル", "違う", "ちがう", "不要", "no"}

// IsDecline reports whether text, apart from surrounding blanks and 。.!！, is one of the words that decline.
func IsDecline(text string) bool {
	return slices.Contains(declineWords, strings.ToLower(strings.Trim(strings.TrimSpace(text), "。.!！ ")))
}

// DeclineAction is the action of the "やめる" button of a card: nil for a card whose declining only closes it (Card.Decline).
func DeclineAction(body domain.CardBody) json.RawMessage {
	for i, option := range body.Options {
		if option == No && i < len(body.OptionActions) && len(body.OptionActions[i]) > 0 && string(body.OptionActions[i]) != "null" {
			return body.OptionActions[i]
		}
	}
	return nil
}

// ActionOf is the action of the option the reply picks (CardBody.OptionActions); ok is false when the reply is none of the options or the option has no action.
func ActionOf(body domain.CardBody, reply string) (action json.RawMessage, ok bool) {
	for i, option := range body.Options {
		if option == strings.TrimSpace(reply) && i < len(body.OptionActions) && len(body.OptionActions[i]) > 0 && string(body.OptionActions[i]) != "null" {
			return body.OptionActions[i], true
		}
	}
	return nil, false
}
