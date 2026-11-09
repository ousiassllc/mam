// Package chatbrief_test tests the answers to the Brief questions given in the chat.
package chatbrief_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

type fixture struct {
	svc *service.Services
	db  *repository.DB
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	return fixture{db: db, svc: servicetest.New(t, service.Deps{DB: db, AssetsDir: filepath.Join(t.TempDir(), "assets")})}
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

// ask puts the question card of a Brief item into the thread, as a thread made before the summary of #1114 holds it.
func (f fixture) ask(t *testing.T, projectID string, field domain.BriefField) domain.ThreadMessage {
	t.Helper()
	card := briefing.Question(field, field.Question())
	card.ID = domain.NewID()
	require.NoError(t, repository.NewProjectRepository(f.db).Apply(context.Background(), repository.Turn{ProjectID: projectID, Messages: []domain.ChatMessage{card}}))
	return f.lastMessage(t, projectID)
}

// The chat answer takes the same Aspect Ratios as the Brief form (9:16 / 16:9 / 1:1): another value is
// not stored and the question is asked again, instead of a generation that silently becomes 16:9.
func TestAspectRatioAnswerOutsideTheBriefRatiosIsAskedAgain(t *testing.T) {
	for _, answer := range []string{"4:5", "21:9 で作りたい", "おまかせ比率", "９：１６"} {
		t.Run(answer, func(t *testing.T) {
			f := newFixture(t)
			p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "動画を作りたい"})
			require.NoError(t, err)
			q := f.ask(t, p.ID, domain.BriefAspectRatio)

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
	q := f.lastMessage(t, p.ID) // the Subject question

	ws := f.post(t, p.ID, strings.Repeat("あ", 201), q.ID)

	assert.Empty(t, f.brief(t, p.ID).Subject)
	require.Len(t, ws.Messages, 2)
	assert.Equal(t, string(domain.BriefSubject), ws.Messages[1].RefID)
	assert.Contains(t, domain.DecodeCardBody(ws.Messages[1].Content).Text, "200 文字以内")
}

// A URL the Brief form refuses (over 200 characters) is not taken from the chat: otherwise the form could
// not save any other item until the user deleted it.
func TestURLLongerThanTheFormLimitIsNotTakenFromTheChatSoTheFormStaysSavable(t *testing.T) {
	long := "https://example.com/?" + strings.Repeat("a", 250)
	short := "https://example.com/ok"
	f := newFixture(t)
	ctx := context.Background()

	// The first message and a later one take the short URL only.
	p, err := f.svc.Projects.Create(ctx, service.CreateProjectInput{Content: "動画を作りたい " + long + " " + short})
	require.NoError(t, err)
	assert.Equal(t, []string{short}, f.brief(t, p.ID).References)
	f.post(t, p.ID, "参考はこれ "+long, "")
	assert.Equal(t, []string{short}, f.brief(t, p.ID).References)

	_, err = f.svc.Projects.UpdateBrief(ctx, p.ID, service.BriefForm{
		Values:     map[domain.BriefField]string{domain.BriefMood: "ポップ"},
		References: strings.Join(f.brief(t, p.ID).References, "\n"),
	})
	require.NoError(t, err)
	assert.Equal(t, "ポップ", f.brief(t, p.ID).Mood)
}

func (f fixture) state(t *testing.T, projectID string) domain.ProjectState {
	t.Helper()
	p, err := f.svc.Projects.Get(context.Background(), projectID)
	require.NoError(t, err)
	return p.State
}
