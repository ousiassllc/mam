package flowtests_test

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// The helpers of the chat flow tests: copies of the helpers of the tests in service/ (that package is at its size limit, so these tests live in a package of their own).

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

const (
	fakeConcept = `{"title":"朝の一滴","logline":"肌が目覚める朝","concept":"朝の光と美容液","tone":"上品","structure":[{"name":"フック","description":"光","duration_seconds":5}]}`
	fakeScript  = `{"title":"脚本","scenes":[{"title":"朝","location":"寝室","description":"光が差す"},{"title":"使う","description":"美容液を手に取る"}]}`
	fakePlan    = `{"scenes":[
		{"title":"朝","location":"寝室","shots":[{"objective":"窓の光","duration_seconds":5},{"objective":"目覚め","duration_seconds":5}]},
		{"title":"使う","shots":[{"objective":"美容液","duration_seconds":5}]}]}`
	fakeBoard = `{"scenes":[{"shots":[{"description":"窓辺の光","framing":"ワイド"},{"description":"目を開ける"}]},{"shots":[{"description":"手のひらの美容液"}]}]}`
)

// fakeReasoner stands in for the Reasoning LLM Adapter: no external API is called. It answers
// per Agent with canned JSON, can fail an Agent on demand and records every call in order.
type fakeReasoner struct {
	mu    sync.Mutex
	calls []llm.Agent
	users map[llm.Agent][]string
	fail  map[llm.Agent]error
}

func (f *fakeReasoner) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, agent)
	f.users[agent] = append(f.users[agent], req.User)
	if err := f.fail[agent]; err != nil {
		return llm.Response{}, err
	}
	text := map[llm.Agent]string{
		llm.AgentProducer: fakeConcept, llm.AgentWriter: fakeScript, llm.AgentDirector: fakePlan, llm.AgentStoryboard: fakeBoard,
	}[agent]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

func (f *fakeReasoner) agents() []llm.Agent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.Agent(nil), f.calls...)
}

func (f *fakeReasoner) count(agent llm.Agent) int {
	n := 0
	for _, a := range f.agents() {
		if a == agent {
			n++
		}
	}
	return n
}

func (f *fakeReasoner) setFailure(agent llm.Agent, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[agent] = err
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

func (f fixture) brief(t *testing.T, projectID string) domain.Brief {
	t.Helper()
	view, err := f.svc.Projects.Brief(context.Background(), projectID)
	require.NoError(t, err)
	return view.Brief
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
	movBytes = append([]byte("\x00\x00\x00\x14ftypqt  "), bytes.Repeat([]byte{0}, 32)...)
)

func newFakeReasoner() *fakeReasoner {
	return &fakeReasoner{users: map[llm.Agent][]string{}, fail: map[llm.Agent]error{}}
}

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
	fake := newFakeReasoner()
	assets := filepath.Join(t.TempDir(), "assets")
	svc := servicetest.New(t, service.Deps{DB: db, Jobs: jobs, LLM: fake, AssetsDir: assets})
	ctx, cancel := context.WithCancel(context.Background())
	jobs.Start(ctx)
	t.Cleanup(func() { cancel(); jobs.Stop() })
	return pipelineFixture{fixture: fixture{svc: svc, db: db, assetsDir: assets}, llm: fake}
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

type pipelineFixture struct {
	fixture
	llm *fakeReasoner
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

func postMessage(t *testing.T, svc *service.Services, projectID string, in service.PostMessageInput) service.Workspace {
	t.Helper()
	ws, err := svc.Chat.PostMessage(context.Background(), projectID, in)
	require.NoError(t, err)
	return ws
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
