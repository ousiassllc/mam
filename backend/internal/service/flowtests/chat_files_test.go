package flowtests_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// offerStub takes the files named *.stub and answers with err (nil: one sentence).
type offerStub struct{ err error }

func (offerStub) Accepts(name string) bool { return strings.HasSuffix(name, ".stub") }

func (o offerStub) Offer(context.Context, domain.Project, string, []chatops.File) ([]domain.ChatMessage, error) {
	if o.err != nil {
		return nil, o.err
	}
	return []domain.ChatMessage{{Role: domain.RoleAssistant, Kind: domain.KindText, Content: "ファイルを受け取りました"}}, nil
}

func openBriefQuestion(t *testing.T, f fixture, projectID string) domain.ThreadMessage {
	t.Helper()
	for _, m := range f.thread(t, projectID) {
		if m.Kind == domain.KindQuestion && m.RefType == domain.RefBriefField && m.Open {
			return m
		}
	}
	t.Fatal("no open Brief question")
	return domain.ThreadMessage{}
}

// A file that an offer takes, sent as the reply to an open Brief question, is not an answer to it: the card stays open (the question was not answered), and an offer that
// fails is told in the thread instead of leaving the message alone there.
func TestChat_AFileSentToAnOfferDoesNotCloseTheBriefQuestionItRepliesTo(t *testing.T) {
	for name, offer := range map[string]offerStub{"offered": {}, "failed": {err: errors.New("disk full")}} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			f.svc.ChatOps().Files(offer)
			p := f.createProject(t, "動画を作りたい")
			question := openBriefQuestion(t, f, p.ID)

			_, err := f.svc.Chat.PostMessage(ctx, p.ID, service.PostMessageInput{ReplyToMessageID: question.ID, Uploads: []service.Upload{upload("a.stub", []byte("x"))}})

			assert.Equal(t, offer.err != nil, err != nil)
			assert.Equal(t, question.ID, openBriefQuestion(t, f, p.ID).ID, "the Brief question was not answered by a file for an offer")
			if offer.err != nil {
				assert.Contains(t, lastMessage(t, f.thread(t, p.ID)).Content, "ファイルを取り込めませんでした", "the thread says what happened")
			}
		})
	}
}
