package service_test

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The helpers of the chat tests that stay in this package (the tests that use them are here; the chat flow tests of service/flowtests carry a copy of what they need).

func answer(t *testing.T, svc *service.Services, projectID, cardID, reply string) service.Workspace {
	t.Helper()
	return postMessage(t, svc, projectID, service.PostMessageInput{Content: reply, ReplyToMessageID: cardID})
}

func ceilings(t *testing.T, svc *service.Services, projectID string) (project, shot *float64) {
	t.Helper()
	view, err := svc.Policy.Settings(context.Background(), projectID)
	require.NoError(t, err)
	return view.Policy.ProjectBudgetCeiling, view.Policy.ShotBudgetCeiling
}

type fixture struct {
	svc       *service.Services
	db        *repository.DB
	assetsDir string
}

func (f fixture) createProject(t *testing.T, content string, uploads ...service.Upload) domain.Project {
	t.Helper()
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: content, Uploads: uploads})
	require.NoError(t, err)
	return p
}

// thread takes a testenv.TestingT so that an Eventually condition can pass its *assert.CollectT: a failure then retries instead of failing the finished test.
func (f fixture) thread(t testenv.TestingT, projectID string) []domain.ThreadMessage {
	t.Helper()
	ws, err := f.svc.Chat.Workspace(context.Background(), projectID)
	require.NoError(t, err)
	return ws.Messages
}

func lastMessage(t testenv.TestingT, msgs []domain.ThreadMessage) domain.ThreadMessage {
	t.Helper()
	require.NotEmpty(t, msgs)
	return msgs[len(msgs)-1]
}

func lastText(ws service.Workspace) string {
	return ws.Messages[len(ws.Messages)-1].Content
}

var (
	pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	mp4Bytes = append([]byte("\x00\x00\x00\x18ftypisom"), bytes.Repeat([]byte{0}, 32)...)
)

func newFixture(t *testing.T) fixture {
	t.Helper()
	testenv.Parallel(t)
	db, assets := testdb.Open(t), filepath.Join(t.TempDir(), "assets")
	return fixture{svc: servicetest.New(t, service.Deps{DB: db, AssetsDir: assets}), db: db, assetsDir: assets}
}

// operationCards are the confirmation cards of the thread, oldest first.
func operationCards(msgs []domain.ThreadMessage) []domain.ThreadMessage {
	var cards []domain.ThreadMessage
	for _, m := range msgs {
		if m.Kind == domain.KindQuestion && m.RefType == domain.RefOperation {
			cards = append(cards, m)
		}
	}
	return cards
}

func requireInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, field, invalid.Field)
}

func upload(name string, data []byte) service.Upload {
	return service.Upload{Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}}
}
