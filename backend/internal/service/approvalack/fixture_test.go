package approvalack_test

import (
	"context"
	"path/filepath"
	"testing"

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
	return fixture{svc: servicetest.New(t, service.Deps{DB: testdb.Open(t), AssetsDir: filepath.Join(t.TempDir(), "assets")})}
}

func (f fixture) createProject(t *testing.T, content string) domain.Project {
	t.Helper()
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: content})
	require.NoError(t, err)
	return p
}

func (f fixture) thread(t *testing.T, projectID string) []domain.ThreadMessage {
	t.Helper()
	ws, err := f.svc.Chat.Workspace(context.Background(), projectID)
	require.NoError(t, err)
	return ws.Messages
}

func lastMessage(t *testing.T, msgs []domain.ThreadMessage) domain.ThreadMessage {
	t.Helper()
	require.NotEmpty(t, msgs)
	return msgs[len(msgs)-1]
}
