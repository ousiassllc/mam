package flowtests_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The helpers of the chat flow tests. The functions that need no fixture are shared with the chat tests of service/ (servicetest/chat.go).
var (
	answer   = servicetest.Answer
	ceilings = servicetest.Ceilings
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

func (f fixture) brief(t *testing.T, projectID string) domain.Brief {
	t.Helper()
	view, err := f.svc.Projects.Brief(context.Background(), projectID)
	require.NoError(t, err)
	return view.Brief
}

var (
	lastMessage = servicetest.LastMessage
	lastText    = servicetest.LastText
	pngBytes    = servicetest.PNGBytes
	mp4Bytes    = servicetest.MP4Bytes
	movBytes    = append([]byte("\x00\x00\x00\x14ftypqt  "), bytes.Repeat([]byte{0}, 32)...)
)

func newFixture(t *testing.T) fixture {
	t.Helper()
	testenv.Parallel(t)
	db, assets := testdb.Open(t), filepath.Join(t.TempDir(), "assets")
	return fixture{svc: servicetest.New(t, service.Deps{DB: db, AssetsDir: assets}), db: db, assetsDir: assets}
}

func newPipelineFixture(t *testing.T) pipelineFixture {
	t.Helper()
	testenv.Parallel(t)
	db := testdb.Open(t)
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	fake := servicetest.NewFakeReasoner()
	assets := filepath.Join(t.TempDir(), "assets")
	svc := servicetest.New(t, service.Deps{DB: db, Jobs: jobs, LLM: fake, AssetsDir: assets})
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })
	return pipelineFixture{fixture: fixture{svc: svc, db: db, assetsDir: assets}, llm: fake}
}

var operationCards = servicetest.OperationCards

type pipelineFixture struct {
	fixture
	llm *servicetest.FakeReasoner
}

// completeBrief fills every Brief item through the Brief form, which starts the pipeline.
func (f pipelineFixture) completeBrief(t *testing.T, projectID string) {
	t.Helper()
	_, err := f.svc.Projects.UpdateBrief(context.Background(), projectID, service.BriefForm{Values: map[domain.BriefField]string{
		domain.BriefContentType: "商品CM", domain.BriefObjective: "Instagram広告", domain.BriefSubject: "美容液",
		domain.BriefTarget: "20代女性", domain.BriefDuration: "15秒", domain.BriefAspectRatio: "9:16", domain.BriefMood: "上品",
	}})
	require.NoError(t, err)
}

func (f pipelineFixture) state(t testenv.TestingT, projectID string) domain.ProjectState {
	t.Helper()
	p, err := f.svc.Projects.Get(context.Background(), projectID)
	require.NoError(t, err)
	return p.State
}

func (f pipelineFixture) waitState(t *testing.T, projectID string, want domain.ProjectState) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, want, f.state(c, projectID), "the Project never reached %s", want)
	}, 5*time.Second, 10*time.Millisecond)
}

func (f pipelineFixture) hierarchy(t testenv.TestingT, projectID string) service.SceneView {
	t.Helper()
	v, err := f.svc.Plans.Hierarchy(context.Background(), projectID, "", "")
	require.NoError(t, err)
	return v
}

func (f pipelineFixture) waitIdle(t *testing.T, projectID string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		ok := true
		for _, m := range f.thread(c, projectID) {
			if m.Kind == domain.KindProgress && (m.Job == nil || (m.Job.Status != domain.JobSucceeded && m.Job.Status != domain.JobFailed)) {
				ok = false
			}
		}
		assert.True(c, ok)
	}, 5*time.Second, 10*time.Millisecond, "a plan job never finished")
}

var (
	postMessage    = servicetest.PostMessage
	requireInvalid = servicetest.RequireInvalid
	upload         = servicetest.Upload
)
