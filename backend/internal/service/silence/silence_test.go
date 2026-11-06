package silence_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
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

// #1107: a message of the user that is stored is never left alone in the thread. The thread gets the AI's
// answer, a Progress card or a failure notice, and the log says why with project_id and message_id.

const (
	conceptJSON = `{"title":"朝の一滴","logline":"肌が目覚める朝","concept":"朝の光と美容液","tone":"上品","structure":[{"name":"フック","description":"光","duration_seconds":5}]}`
	scriptJSON  = `{"title":"脚本","scenes":[{"title":"朝","location":"寝室","description":"光が差す"}]}`
)

// reasoner stands in for the Reasoning LLM Adapter (no external API): it answers the Producer and the Writer,
// and panics when asked to (panicking).
type reasoner struct{ panicking bool }

func (r reasoner) Complete(_ context.Context, agent llm.Agent, _ llm.Request) (llm.Response, error) {
	if r.panicking {
		panic("boom")
	}
	text := map[llm.Agent]string{llm.AgentProducer: conceptJSON, llm.AgentWriter: scriptJSON}[agent]
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// logBuffer is a log destination that the test reads while the services write to it.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fixture struct {
	svc  *service.Services
	db   *repository.DB
	logs *logBuffer
}

// newFixture builds the services; llmOn false leaves out the LLM and the queue, so nothing drives the AUTO pipeline.
func newFixture(t *testing.T, llmOn bool, r reasoner) fixture {
	t.Helper()
	testenv.Parallel(t)
	db, logs := testdb.Open(t), &logBuffer{}
	deps := service.Deps{DB: db, AssetsDir: filepath.Join(t.TempDir(), "assets"), Logger: slog.New(slog.NewTextHandler(logs, nil))}
	var jobs *jobqueue.Queue
	if llmOn {
		jobs = jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
		deps.Jobs, deps.LLM = jobs, r
	}
	svc := servicetest.New(t, deps)
	if jobs != nil { // the workers start once the services registered their handlers
		ctx, cancel := context.WithCancel(context.Background())
		jobs.Start(ctx)
		t.Cleanup(func() { cancel(); jobs.Stop() })
	}
	return fixture{svc: svc, db: db, logs: logs}
}

func (f fixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	require.NoError(t, f.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// newProject creates a Project and fills its Brief, which starts the pipeline when there is one.
func (f fixture) newProject(t *testing.T) domain.Project {
	t.Helper()
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい"})
	require.NoError(t, err)
	_, err = f.svc.Projects.UpdateBrief(context.Background(), p.ID, service.BriefForm{Values: map[domain.BriefField]string{
		domain.BriefContentType: "商品CM", domain.BriefObjective: "Instagram広告", domain.BriefSubject: "美容液",
		domain.BriefTarget: "20代女性", domain.BriefDuration: "15秒", domain.BriefAspectRatio: "9:16", domain.BriefMood: "上品",
	}})
	require.NoError(t, err)
	return p
}

func (f fixture) thread(t testenv.TestingT, projectID string) []domain.ThreadMessage {
	t.Helper()
	ws, err := f.svc.Chat.Workspace(context.Background(), projectID)
	require.NoError(t, err)
	return ws.Messages
}

func (f fixture) state(t testenv.TestingT, projectID string) domain.ProjectState {
	t.Helper()
	p, err := f.svc.Projects.Get(context.Background(), projectID)
	require.NoError(t, err)
	return p.State
}

// say posts a free message and returns the stored message of the user, what follows it in the thread and the response.
func (f fixture) say(t *testing.T, projectID, content string) (domain.ThreadMessage, []domain.ThreadMessage, service.Workspace) {
	t.Helper()
	ws, err := f.svc.Chat.PostMessage(context.Background(), projectID, service.PostMessageInput{Content: content})
	require.NoError(t, err)
	require.NotEmpty(t, ws.Messages)
	user := ws.Messages[0]
	require.Equal(t, domain.RoleUser, user.Role)
	var after []domain.ThreadMessage
	for _, m := range f.thread(t, projectID) {
		if m.ID > user.ID { // IDs are time-ordered
			after = append(after, m)
		}
	}
	return user, after, ws
}

// waitIdle waits until no Progress card of the thread is still running.
func (f fixture) waitIdle(t *testing.T, projectID string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		for _, m := range f.thread(c, projectID) {
			if m.Kind == domain.KindProgress {
				assert.True(c, m.Job != nil && (m.Job.Status == domain.JobSucceeded || m.Job.Status == domain.JobFailed))
			}
		}
	}, 5*time.Second, 10*time.Millisecond, "a plan job never finished")
}

func (f fixture) waitState(t *testing.T, projectID string, want domain.ProjectState) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) { assert.Equal(c, want, f.state(c, projectID)) }, 5*time.Second, 10*time.Millisecond)
}

func TestAMessageThatStartsNothingIsAnsweredWithTheReason(t *testing.T) {
	f := newFixture(t, false, reasoner{}) // no LLM: nothing drives the AUTO pipeline
	p := f.newProject(t)
	require.Equal(t, domain.StateDraft, f.state(t, p.ID), "the Brief is complete but no pipeline moves the Project")

	user, after, ws := f.say(t, p.ID, "続けて")
	require.Len(t, after, 1, "the user's words are not the last of the thread")
	assert.Equal(t, domain.RoleAssistant, after[0].Role)
	assert.Contains(t, after[0].Content, "保存しました")
	require.Len(t, ws.Messages, 2, "the response carries the notice, so that the chat shows it at once")
	assert.Equal(t, after[0].ID, ws.Messages[1].ID)
	assert.Equal(t, domain.RefGlobalSettings, after[0].RefType, "the notice links to the LLM settings")
	assert.Contains(t, f.logs.String(), "message_id="+user.ID)
	assert.Contains(t, f.logs.String(), "project_id="+p.ID)
}

func TestAMessageWhileWaitingForAnApprovalIsAnsweredWithTheReason(t *testing.T) {
	f := newFixture(t, true, reasoner{})
	p := f.newProject(t)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	f.waitIdle(t, p.ID)

	_, after, _ := f.say(t, p.ID, "続けて")
	require.Len(t, after, 1)
	assert.Equal(t, domain.RoleAssistant, after[0].Role)
	assert.Contains(t, after[0].Content, "承認")
}

func TestAFailureToStartThePipelineIsToldInTheThreadAndLogged(t *testing.T) {
	f := newFixture(t, true, reasoner{})
	f.exec(t, `CREATE TRIGGER no_jobs BEFORE INSERT ON job BEGIN SELECT RAISE(ABORT, 'queue is full'); END`)
	p := f.newProject(t)
	require.Equal(t, domain.StateDraft, f.state(t, p.ID))

	user, after, _ := f.say(t, p.ID, "続けて")
	require.Len(t, after, 1)
	assert.Equal(t, domain.RoleAssistant, after[0].Role)
	assert.Contains(t, after[0].Content, "制作を始められませんでした")
	logs := f.logs.String()
	assert.Contains(t, logs, "message_id="+user.ID)
	assert.Contains(t, logs, "project_id="+p.ID)
	assert.Contains(t, logs, "queue is full", "the reason is in the log")
}

func TestAStartedPipelineAnswersWithAProgressCardNotWithANotice(t *testing.T) {
	f := newFixture(t, true, reasoner{})
	p := f.newProject(t)
	f.waitState(t, p.ID, domain.StateAwaitingApproval)
	f.waitIdle(t, p.ID)
	f.exec(t, `UPDATE project SET state = 'PLANNING' WHERE id = ?`, p.ID)

	_, after, _ := f.say(t, p.ID, "続けて")
	require.NotEmpty(t, after)
	assert.Equal(t, domain.KindProgress, after[0].Kind, "the plan job's Progress card follows the message")
	for _, m := range after {
		assert.NotContains(t, m.Content, "保存しました", "no notice: the pipeline answered")
	}
	f.waitIdle(t, p.ID)
}

func TestAPlanJobPanicIsToldInTheThreadAndLoggedWithTheProject(t *testing.T) {
	f := newFixture(t, true, reasoner{panicking: true})
	p := f.newProject(t)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		msgs := f.thread(c, p.ID)
		require.NotEmpty(c, msgs)
		assert.Contains(c, msgs[len(msgs)-1].Content, "制作を続けられませんでした")
	}, 5*time.Second, 10*time.Millisecond)
	f.waitIdle(t, p.ID)
	assert.Contains(t, f.logs.String(), "planning pipeline panicked")
	assert.Contains(t, f.logs.String(), "project_id="+p.ID)
}
