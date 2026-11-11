// Package chatops is the contract between the chat and the operations that change the global settings of the app from it (#1140: an API key, a model install,
// an assignment, a LoRA, an export, the reference of a Character). Each operation is a package of its own under this one: its commandinterpreter.Spec (what the
// model is told, what it may return), its Handler (the card that asks before anything happens, and what the answer carries out through the service layer the
// screen and MCP use), and its tests with fakes. The chat (service/director_ops.go, chat_operation.go) knows only this package: it posts the card of Question,
// carries out Apply when the user accepts it, and never touches what an operation does.
package chatops

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Card is the confirmation card of an operation: what will be done and what it affects. Question never changes anything.
type Card struct {
	// Text is the sentence of the card. An operation on the global settings says that it affects every Project.
	Text string
	// Choices make the card offer several operations instead of one yes: a request that names several models asks which one, each button carries its own
	// operation. Without choices the card has "はい、この内容で進める" (the operation asked for) and "やめる".
	Choices []Choice
	// Decline is the operation the "やめる" button (or any word that declines) carries out when the card has something to clean up (a file that waits for the answer).
	// Its text, when it has one, replaces "やめました。何も変更していません。". nil: declining only closes the card.
	Decline *commandinterpreter.Operation
	// Op is the operation the yes button carries when it is not the one asked: Question fills in what only this turn knows (the asset a message attached), since the
	// button's answer comes later and carries no attachment. nil: the operation as asked.
	Op *commandinterpreter.Operation
	// Secret makes the card ask for a secret in a masked field of its own (an API key). The value is posted to the dedicated endpoint and handed to
	// SecretSubmitter.Submit; it is never a chat message, an operation or a log line. A card with a secret has no yes button.
	Secret *domain.SecretField
}

// Choice is one button of a card that offers several operations.
type Choice struct {
	Label string
	Op    commandinterpreter.Operation
}

// Result is what an operation did: the sentence for the thread, and the messages that follow it (a progress card of an install, a card that asks for the key
// the provider just chosen needs).
type Result struct {
	Text     string
	Messages []domain.ChatMessage
}

// Handler is the chat side of one operation.
type Handler interface {
	// Question builds the card that asks before the operation is carried out. It returns ErrNoChange when the operation would change nothing and a *Refusal
	// when it cannot be done (the sentence says why). An operation that is Spec.Immediate is not asked.
	Question(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (Card, error)
	// Apply carries the operation out, as the user confirmed it or as an Immediate operation asked, through the same service functions as the MCP tool. A *Refusal is
	// a reason the user can act on.
	Apply(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (Result, error)
}

// SecretSubmitter is implemented by the Handler of a card that asks for a secret.
type SecretSubmitter interface {
	// Submit stores the secret the user typed into the card's masked field. It MUST NOT keep, log or return the value.
	Submit(ctx context.Context, project domain.Project, field domain.SecretField, secret string) (Result, error)
}

// File is a file a message carries that no attachment of the Brief takes (a LoRA).
type File struct {
	Name string
	Size int64
	// Open is called at most once.
	Open func() (io.ReadCloser, error)
}

// FileOffer reads the files of a message that are not images or videos: the chat hands them over instead of refusing them as an unsupported attachment.
type FileOffer interface {
	// Accepts reports whether the file with this name is one the offer takes (by its extension: the content is checked by Offer).
	Accepts(name string) bool
	// Offer checks the files (all of which Accepts) and returns the messages that answer them: the card that asks before they are used, or the sentence that says why
	// not. message is what the user wrote with them. The files are kept by the offer until the card is answered; nothing is changed before.
	Offer(ctx context.Context, project domain.Project, message string, files []File) ([]domain.ChatMessage, error)
}

// ProgressDetail says how far the work behind a progress card is ("42% ・ダウンロード中"); "" when it does not know the job.
type ProgressDetail func(ctx context.Context, jobID string) string

// ErrNoChange is the answer of Question that finds the operation already done.
var ErrNoChange = errors.New("chatops: the operation changes nothing")

// Refusal is a reason the user can act on: the operation cannot be done (the key is unknown, the settings file is fixed by a start option, the model is
// not installed). Message is told as it is.
type Refusal struct{ Message string }

func (r *Refusal) Error() string { return "chatops: " + r.Message }

// Refuse is a *Refusal with the formatted message.
func Refuse(format string, args ...any) error { return &Refusal{Message: fmt.Sprintf(format, args...)} }

type attachmentsKey struct{}

// WithAttachments puts the assets the message of this turn carries into ctx, for the operations that act on them (a face for a Character).
func WithAttachments(ctx context.Context, assets []domain.Asset) context.Context {
	return context.WithValue(ctx, attachmentsKey{}, assets)
}

// Attachments are the assets the message of this turn carries (WithAttachments); none on a button's answer.
func Attachments(ctx context.Context) []domain.Asset {
	assets, _ := ctx.Value(attachmentsKey{}).([]domain.Asset)
	return assets
}
