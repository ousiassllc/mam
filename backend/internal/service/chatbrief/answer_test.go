// Package chatbrief_test tests the answers to the Brief questions given in the chat.
package chatbrief_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

type fixture struct{ svc *service.Services }

func newFixture(t *testing.T) fixture {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	return fixture{svc: servicetest.New(t, service.Deps{DB: db, AssetsDir: filepath.Join(t.TempDir(), "assets")})}
}

func (f fixture) post(t *testing.T, projectID, content, replyTo string) service.Workspace {
	t.Helper()
	ws, err := f.svc.Chat.PostMessage(context.Background(), projectID, service.PostMessageInput{Content: content, ReplyToMessageID: replyTo})
	require.NoError(t, err)
	return ws
}

func (f fixture) brief(t *testing.T, projectID string) domain.Brief {
	t.Helper()
	view, err := f.svc.Projects.Brief(context.Background(), projectID)
	require.NoError(t, err)
	return view.Brief
}

func (f fixture) lastMessage(t *testing.T, projectID string) domain.ThreadMessage {
	t.Helper()
	ws, err := f.svc.Chat.Workspace(context.Background(), projectID)
	require.NoError(t, err)
	require.NotEmpty(t, ws.Messages)
	return ws.Messages[len(ws.Messages)-1]
}

// askUntil delegates the Brief items one by one until the AI asks about field, and returns that card.
func (f fixture) askUntil(t *testing.T, projectID string, field domain.BriefField) domain.ThreadMessage {
	t.Helper()
	for range domain.RequiredBriefFields() {
		q := f.lastMessage(t, projectID)
		require.Equal(t, domain.KindQuestion, q.Kind)
		if q.RefID == string(field) {
			return q
		}
		f.post(t, projectID, domain.DelegateOption, q.ID)
	}
	require.FailNow(t, "the AI never asked about "+string(field))
	return domain.ThreadMessage{}
}

// The chat answer takes the same Aspect Ratios as the Brief form (9:16 / 16:9 / 1:1): another value is
// not stored and the question is asked again, instead of a generation that silently becomes 16:9.
func TestAspectRatioAnswerOutsideTheBriefRatiosIsAskedAgain(t *testing.T) {
	for _, answer := range []string{"4:5", "21:9 で作りたい", "おまかせ比率", "９：１６"} {
		t.Run(answer, func(t *testing.T) {
			f := newFixture(t)
			p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "動画を作りたい"})
			require.NoError(t, err)
			q := f.askUntil(t, p.ID, domain.BriefAspectRatio)

			// A reply to the card.
			ws := f.post(t, p.ID, answer, q.ID)
			assert.Empty(t, f.brief(t, p.ID).AspectRatio, "a ratio the Brief cannot hold is not stored")
			assert.Equal(t, domain.StateDraft, ws.Project.State, "PLANNING does not start on a refused value")
			require.Len(t, ws.Messages, 2, "the user's answer and the question again")
			again := ws.Messages[1]
			assert.Equal(t, domain.KindQuestion, again.Kind)
			assert.Equal(t, string(domain.BriefAspectRatio), again.RefID)
			assert.True(t, again.Open)
			body := domain.DecodeCardBody(again.Content)
			assert.Contains(t, body.Text, "9:16 / 16:9 / 1:1")
			assert.Equal(t, domain.BriefAspectRatios(), body.Options)

			// Free input answers the newest open question: the new card.
			f.post(t, p.ID, answer, "")
			assert.Empty(t, f.brief(t, p.ID).AspectRatio)

			f.post(t, p.ID, "1:1", f.lastMessage(t, p.ID).ID)
			assert.Equal(t, "1:1", f.brief(t, p.ID).AspectRatio, "a quick answer is taken")
		})
	}
}

func TestAnswerLongerThanTheFormLimitIsAskedAgain(t *testing.T) {
	f := newFixture(t)
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "動画を作りたい"})
	require.NoError(t, err)
	q := f.askUntil(t, p.ID, domain.BriefContentType)

	ws := f.post(t, p.ID, strings.Repeat("あ", 201), q.ID)

	assert.Empty(t, f.brief(t, p.ID).ContentType)
	require.Len(t, ws.Messages, 2)
	assert.Equal(t, string(domain.BriefContentType), ws.Messages[1].RefID)
	assert.Contains(t, domain.DecodeCardBody(ws.Messages[1].Content).Text, "200 文字以内")
}
