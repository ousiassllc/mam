package jobs_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/handler/jobs"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// With no job the page says so, in the frame of the app with the menu entry of the job list current, and the refresh button reads the list.
func TestPage_WithNoJobSaysSoAndHasARefreshButton(t *testing.T) {
	s := screentest.New(t)

	rec := s.Visit(paths.Jobs())

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `<h1`)
	assert.Contains(t, body, `data-testid="jobs-empty"`)
	assert.NotContains(t, body, `data-testid="jobs-item"`)
	assert.Contains(t, body, `data-active="false"`)
	assert.NotContains(t, body, "hx-trigger", "nothing runs, so the list does not ask again")
	assert.Regexp(t, `<button[^>]*id="jobs-list-refresh"[^>]*hx-get="`+paths.JobsList()+`"[^>]*hx-target="#jobs-list"[^>]*hx-swap="outerHTML"`, body)
	assert.Contains(t, body, `aria-label="更新"`)
	assert.Regexp(t, `最終更新 <time>\d\d:\d\d:\d\d</time>`, body)
}

// An install that runs is listed with its kind, state, start, the model's name, how far it is and a link to the model manager; the refresh
// button reads the list again, and the list asks for itself while the job runs and stops when it has ended.
func TestList_ARunningInstallIsListedAndTheRefreshButtonReadsItAgain(t *testing.T) {
	s := screentest.New(t)
	s.Rig.InstallEngine()
	hold := s.Rig.Host.Hold("/" + mmtest.CkptA.Folder + "/" + mmtest.CkptA.Name)
	rec := s.Do(http.MethodPost, paths.ModelManagerPart("models", "m-a", "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	job := screentest.JobID(t, rec.Body.String())
	s.Rig.Start()
	hold.Reached(t)

	page := s.Visit(paths.Jobs()).Body.String()
	assert.Contains(t, page, `data-testid="jobs-item"`)
	assert.Contains(t, page, "導入")
	assert.Contains(t, page, "実行中")
	assert.Contains(t, page, `data-testid="jobs-subject">A<`, "the name of the model")
	assert.Contains(t, page, `data-testid="jobs-progress"`)
	assert.Regexp(t, `<progress[^>]*aria-label="導入 A（[^"]*）の進み具合"`, page)
	assert.Regexp(t, `href="`+paths.ModelManager()+`"[^>]*aria-label="開く: 導入 A（`, page)
	assert.Contains(t, page, `data-active="true"`)
	assert.Contains(t, page, `hx-trigger="every 2s"`)
	assert.Contains(t, page, "実行中 1 件")

	refresh := regexp.MustCompile(`<button[^>]*id="jobs-list-refresh"[^>]*hx-get="([^"]+)"`).FindStringSubmatch(page)
	require.Len(t, refresh, 2)
	part := s.Get(refresh[1])
	require.Equal(t, http.StatusOK, part.Code)
	assert.NotContains(t, part.Body.String(), "<html", "the button swaps the list alone")
	assert.Contains(t, part.Body.String(), `id="jobs-list"`)
	assert.Contains(t, part.Body.String(), `id="jobs-list-refresh"`, "and brings the button back")
	assert.Contains(t, part.Body.String(), "実行中")

	hold.Release()
	s.WaitJob(job)
	done := s.Get(paths.JobsList()).Body.String()
	assert.Contains(t, done, "完了")
	assert.NotContains(t, done, `data-testid="jobs-progress"`)
	assert.Contains(t, done, `data-active="false"`)
	assert.NotContains(t, done, "hx-trigger", "no job runs any more, so it stops asking")
	assert.Contains(t, done, `data-testid="refresh"`, "the button stays for the reader")
}

// A job that failed shows why; the list never offers a button that changes a job.
func TestList_AFailedJobShowsItsReasonAndOffersNoAction(t *testing.T) {
	s := screentest.New(t)
	job, err := s.Rig.Table.Create(t.Context(), domain.JobModelInstall, []byte(`{"target":"engine"}`))
	require.NoError(t, err)
	require.NoError(t, s.Rig.Table.MarkFailed(t.Context(), job.ID, "ディスクの空きが足りません"))

	body := s.Get(paths.JobsList()).Body.String()

	assert.Contains(t, body, "失敗")
	assert.Contains(t, body, `data-testid="jobs-failure">ディスクの空きが足りません<`)
	assert.Contains(t, body, "ComfyUI エンジン")
	assert.Equal(t, 1, strings.Count(body, "<button"), "only the refresh button: a job is not stopped from here")
	for _, attr := range []string{"hx-post", "hx-put", "hx-delete"} {
		assert.NotContains(t, body, attr)
	}
}

// A planning job that stopped for want of an LLM key says, under its reason, what to do to go on (the reason itself is not rewritten).
func TestList_AFailedPlanForWantOfAKeyShowsWhatToDoNext(t *testing.T) {
	s := screentest.New(t)
	job, err := s.Rig.Table.Create(t.Context(), domain.JobPlan, []byte(`{"project_id":"gone"}`))
	require.NoError(t, err)
	require.NoError(t, s.Rig.Table.MarkFailed(t.Context(), job.ID, "LLM の API キーが設定されていません。失敗した Agent は Producer です。"))

	body := s.Get(paths.JobsList()).Body.String()

	assert.Contains(t, body, `data-testid="jobs-failure">LLM の API キーが設定されていません。失敗した Agent は Producer です。<`)
	assert.Contains(t, body, `data-testid="jobs-next-steps"`)
	assert.Contains(t, body, "登場人物を追加")
}

type failingTable struct{}

func (failingTable) ListRecent(context.Context, int) ([]domain.Job, error) {
	return nil, errors.New("database is locked")
}

// A table that cannot be read leaves a message in the list, with the button to try again, and not the cause (it goes to the log).
func TestList_AnUnreadableTableIsAMessageInTheListAndNotAnErrorPage(t *testing.T) {
	engine := gin.New()
	jobs.Register(engine, joblist.New(joblist.Deps{Table: failingTable{}}))

	for _, path := range []string{paths.Jobs(), paths.JobsList()} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		require.Equal(t, http.StatusOK, rec.Code, path)
		body := rec.Body.String()
		assert.Contains(t, body, `role="alert"`, path)
		assert.Contains(t, body, `data-testid="jobs-error"`, path)
		assert.NotContains(t, body, "database is locked", "the cause stays out of the page")
		assert.NotContains(t, body, `data-testid="jobs-empty"`, "a list that could not be read is not an empty one")
		assert.Contains(t, body, `id="jobs-list-refresh"`, path)
		assert.Contains(t, body, `hx-trigger="every 2s"`, "the polling is kept, so the list comes back when the table can be read")
	}
}

// The works that have no row in the job table are rows of the same list: the same row (kind, state, start, subject, a link to where it is
// worked on, "開く" with a name), the same polling while one runs and the same stop when all ended. One that cannot say how far it is shows
// its words and the time it has run and no bar; one with a percentage has the bar; a long name wraps like the others.
func TestList_ATrackedWorkIsARowThatPollsWhileItRunsAndStopsWhenItEnds(t *testing.T) {
	s := screentest.New(t)
	p, err := s.Svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "コーヒー店の動画を作りたい"})
	require.NoError(t, err)
	render := s.Works.Begin(worktrack.Record{Kind: worktrack.KindExport, ProjectID: p.ID}, "動画を書き出しています")
	long := strings.Repeat("とても長い名前の", 20)
	run := s.Works.Begin(worktrack.Record{Kind: worktrack.KindWorkflowRun, Subject: long}, "ComfyUI を準備しています")
	run.Progress(30, "KSampler")

	page := s.Visit(paths.Jobs()).Body.String()
	assert.Equal(t, 2, strings.Count(page, `data-testid="jobs-item"`))
	assert.Contains(t, page, "動画の書き出し")
	assert.Contains(t, page, "ワークフローの実行")
	assert.Contains(t, page, "実行中 2 件")
	assert.Contains(t, page, `hx-trigger="every 2s"`)
	assert.Regexp(t, `<p class="break-words[^"]*" data-testid="jobs-subject">`+long+`<`, page, "a long name wraps")
	assert.Regexp(t, `href="`+paths.Project(p.ID, "edit")+`"[^>]*aria-label="開く: 動画の書き出し `+p.Name+`（`, page)
	assert.Regexp(t, `href="`+paths.Workflows()+`"[^>]*aria-label="開く: ワークフローの実行 `, page)
	assert.Regexp(t, `動画を書き出しています・経過 \d+秒`, page, "no bar for a render: its words and the time it has run")
	assert.Equal(t, 1, strings.Count(page, "<progress"), "only the work that knows its percentage has a bar")
	assert.Regexp(t, `<progress[^>]*value="30"[^>]*aria-label="ワークフローの実行 `, page)

	render.Succeed()
	run.Fail("out of memory")
	done := s.Get(paths.JobsList()).Body.String()
	assert.Contains(t, done, "完了")
	assert.Contains(t, done, `data-testid="jobs-failure">out of memory<`)
	assert.NotContains(t, done, `data-testid="jobs-progress"`)
	assert.Contains(t, done, `data-active="false"`)
	assert.NotContains(t, done, "hx-trigger", "all ended: the list stops asking")
}
