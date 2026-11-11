package service_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The helpers of the chat tests that stay in this package. The functions that need no fixture are shared with the chat flow tests of service/flowtests (servicetest/chat.go).
var (
	answer         = servicetest.Answer
	ceilings       = servicetest.Ceilings
	lastMessage    = servicetest.LastMessage
	lastText       = servicetest.LastText
	operationCards = servicetest.OperationCards
	requireInvalid = servicetest.RequireInvalid
	upload         = servicetest.Upload
	postMessage    = servicetest.PostMessage
	pngBytes       = servicetest.PNGBytes
	mp4Bytes       = servicetest.MP4Bytes
)

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

func newFixture(t *testing.T) fixture {
	t.Helper()
	testenv.Parallel(t)
	db, assets := testdb.Open(t), filepath.Join(t.TempDir(), "assets")
	return fixture{svc: servicetest.New(t, service.Deps{DB: db, AssetsDir: assets}), db: db, assetsDir: assets}
}
