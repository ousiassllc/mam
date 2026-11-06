package joblist_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/planadvice"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobrecent"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

func TestMain(m *testing.M) { mmtest.Main(m) }

// world is a job table next to the services that say what the jobs work on.
type world struct {
	t   *testing.T
	db  *repository.DB
	rig *mmtest.Rig
	svc *service.Services
	// list reads the table of the model manager's rig, which is where its jobs and the jobs of its generation test are.
	list *joblist.Service
}

func newWorld(t *testing.T) *world {
	t.Helper()
	rig := mmtest.New(t)
	db := testdb.Open(t)
	svc := servicetest.New(t, service.Deps{DB: db, Credentials: credentialtest.New()})
	return &world{t: t, db: db, rig: rig, svc: svc, list: joblist.New(joblist.Deps{
		Table: jobrecent.New(rig.DB()), Shots: repository.NewGenerationRepository(db), Projects: svc.Projects, ModelManager: rig.Svc, Gentest: rig.Gentest,
	})}
}

func (w *world) recent() []joblist.Entry {
	w.t.Helper()
	entries, err := w.list.Recent(context.Background())
	require.NoError(w.t, err)
	return entries
}

func (w *world) create(kind domain.JobKind, payload any) domain.Job {
	w.t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(w.t, err)
	job, err := w.rig.Table.Create(context.Background(), kind, raw)
	require.NoError(w.t, err)
	return job
}

func (w *world) exec(queries ...string) {
	w.t.Helper()
	require.NoError(w.t, w.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range queries {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

// An install is listed with the model it installs and a way to the model manager; it ends as succeeded with its end time, and the list
// tells how far a running one is.
func TestRecent_AnInstallNamesItsModelAndSaysHowFarItIs(t *testing.T) {
	w := newWorld(t)
	w.rig.InstallEngine()
	hold := w.rig.Host.Hold("/" + mmtest.CkptA.Folder + "/" + mmtest.CkptA.Name)
	ref, err := w.rig.Svc.InstallModel(context.Background(), "m-a", nil)
	require.NoError(t, err)

	w.rig.Start()
	hold.Reached(t)
	running := w.recent()[0]
	assert.Equal(t, ref.JobID, running.ID)
	assert.Equal(t, "導入", running.Label)
	assert.Equal(t, "A", running.Subject, "the name of the model in the catalog")
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenModelManager, Category: joblist.CategoryImageVideo}, running.Dest)
	assert.Equal(t, joblist.Running, running.State)
	require.NotNil(t, running.Progress, "a running install says how far it is")
	assert.GreaterOrEqual(t, running.Progress.Percent, 0)

	hold.Release()
	w.rig.Wait(ref.JobID)
	done := w.recent()[0]
	assert.Equal(t, joblist.Succeeded, done.State)
	assert.NotNil(t, done.FinishedAt)
	assert.Nil(t, done.Progress, "a job that ended has no progress")
}

// The engine and a model that is not in the catalog have their own subject; a job whose payload names nothing has none.
func TestRecent_TheEngineHasANameAndAnUnknownModelHasNone(t *testing.T) {
	w := newWorld(t)
	engine := w.create(domain.JobModelInstall, map[string]string{"target": "engine"})
	gone := w.create(domain.JobModelInstall, map[string]string{"target": "model:no-such-model"})
	blank := w.create(domain.JobModelInstall, map[string]string{})

	subjects := map[string]string{}
	for _, e := range w.recent() {
		subjects[e.ID] = e.Subject
		assert.Equal(t, joblist.Queued, e.State, "nothing has started the queue")
		assert.Nil(t, e.Progress, "a job that waits has no progress")
	}
	assert.Equal(t, "ComfyUI エンジン", subjects[engine.ID])
	assert.Empty(t, subjects[gone.ID])
	assert.Empty(t, subjects[blank.ID])
}

// The age gate closed (Issue #992): an import from Hugging Face of a repository for adults (NFAA) does not name the repository, as a catalog model
// for adults has no name here; the name comes back when the gate opens, and an import of a general repository is named either way.
func TestRecent_AnHFImportOfAnAdultRepositoryHasNoNameWhileTheGateIsClosed(t *testing.T) {
	w := newWorld(t)
	pin := func(repo string, nfaa bool) map[string]any {
		return map[string]any{
			"target": "hf:" + repo + "@abc/x.safetensors",
			"hf":     map[string]any{"repo": repo, "commit": "abc", "path": "x.safetensors", "folder": "checkpoints", "name": "x-" + repo + ".safetensors", "nfaa": nfaa},
		}
	}
	adult := w.create(domain.JobModelInstall, pin("o/adult", true))
	general := w.create(domain.JobModelInstall, pin("o/general", false))
	subjects := func() map[string]string {
		out := map[string]string{}
		for _, e := range w.recent() {
			out[e.ID] = e.Subject
		}
		return out
	}

	closed := subjects()
	assert.Empty(t, closed[adult.ID], "the repository of an adult model is not named while the gate is closed")
	assert.Equal(t, "o/general", closed[general.ID])

	w.rig.ConfirmAdult()
	open := subjects()
	assert.Equal(t, "o/adult", open[adult.ID])
	assert.Equal(t, "o/general", open[general.ID])
}

// The work on a Shot leads to the Scene / Shot screen of its Project and names the Project; a failure is shown with its reason (secrets
// hidden). A Shot or a Project that is gone is not a fault of the list.
func TestRecent_AShotJobLeadsToTheProjectAndAFailureSaysWhy(t *testing.T) {
	w := newWorld(t)
	p, err := w.svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "コーヒー店の動画を作りたい"})
	require.NoError(t, err)
	w.exec(
		`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', '`+p.ID+`', 'S', 0)`,
		`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 4, 0)`,
	)
	ok := w.create(domain.JobGenerate, map[string]string{"shot_id": "sh1"})
	failed := w.create(domain.JobJudge, map[string]string{"shot_id": "sh1", "take_id": "t1"})
	require.NoError(t, w.rig.Table.MarkFailed(t.Context(), failed.ID, "upstream: key sk-proj-abcdefghijklmnopqrstuvwxyz0123 was refused"))
	orphan := w.create(domain.JobRepair, map[string]string{"shot_id": "no-such-shot"})
	plan := w.create(domain.JobPlan, map[string]string{"project_id": p.ID})

	byID := map[string]joblist.Entry{}
	for _, e := range w.recent() {
		byID[e.ID] = e
	}
	assert.Equal(t, "Shot の生成", byID[ok.ID].Label)
	assert.Equal(t, p.Name, byID[ok.ID].Subject)
	assert.NotEmpty(t, p.Name)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenProjectScenes, ProjectID: p.ID}, byID[ok.ID].Dest)
	assert.Equal(t, joblist.Failed, byID[failed.ID].State)
	assert.Contains(t, byID[failed.ID].Error, "upstream")
	assert.NotContains(t, byID[failed.ID].Error, "sk-proj-abcdefghijklmnopqrstuvwxyz0123", "a key in a reason is hidden")
	assert.Equal(t, joblist.Dest{}, byID[orphan.ID].Dest, "a Shot that is gone leads nowhere")
	assert.Empty(t, byID[orphan.ID].Subject)
	assert.Equal(t, "企画・脚本の作成", byID[plan.ID].Label)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenProjectChat, ProjectID: p.ID}, byID[plan.ID].Dest)
}

// A job that kept its failure as the common error (#1010: job.error_info) hands it on in words, for the screen's error box; a job whose
// failure is only a sentence (an older row) has none.
func TestRecent_AFailedJobCarriesItsCommonError(t *testing.T) {
	w := newWorld(t)
	kept := w.create(domain.JobModelInstall, map[string]string{"target": "engine"})
	e := apperr.New(apperr.CodeDiskFull).WithDetail("fetch: no space left on device")
	require.NoError(t, w.rig.Table.MarkFailedWithInfo(t.Context(), kept.ID, "ディスクの空きが足りません", e.Marshal()))
	old := w.create(domain.JobModelInstall, map[string]string{"target": "engine"})
	require.NoError(t, w.rig.Table.MarkFailed(t.Context(), old.ID, "ディスクの空きが足りません"))

	byID := map[string]joblist.Entry{}
	for _, e := range w.recent() {
		byID[e.ID] = e
	}
	require.NotNil(t, byID[kept.ID].Failure)
	assert.Equal(t, apperr.CodeDiskFull, byID[kept.ID].Failure.Code)
	assert.Equal(t, "fetch: no space left on device", byID[kept.ID].Failure.Detail)
	assert.NotEmpty(t, byID[kept.ID].Failure.Remedy)
	assert.Equal(t, "ディスクの空きが足りません", byID[kept.ID].Error)
	assert.Nil(t, byID[old.ID].Failure)
}

// A planning job that stopped for want of an LLM key carries what to do to go on, in the words of the screen; one that stopped
// for another reason, and a job of another kind, carry none.
func TestRecent_AFailedPlanForWantOfAKeyHasNextSteps(t *testing.T) {
	w := newWorld(t)
	p, err := w.svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "コーヒー店の動画を作りたい"})
	require.NoError(t, err)
	noKey := w.create(domain.JobPlan, map[string]string{"project_id": p.ID})
	require.NoError(t, w.rig.Table.MarkFailed(t.Context(), noKey.ID, "LLM の API キーが設定されていません。失敗した Agent は Producer（コンセプト）です。"))
	timeout := w.create(domain.JobPlan, map[string]string{"project_id": p.ID})
	require.NoError(t, w.rig.Table.MarkFailed(t.Context(), timeout.ID, "AI の応答がタイムアウトしました"))
	queued := w.create(domain.JobPlan, map[string]string{"project_id": p.ID})

	byID := map[string]joblist.Entry{}
	for _, e := range w.recent() {
		byID[e.ID] = e
	}
	assert.Contains(t, byID[noKey.ID].NextSteps, "登場人物を追加")
	assert.Contains(t, byID[noKey.ID].Error, "API キー", "the failure text stays as it was written")
	assert.Empty(t, byID[timeout.ID].NextSteps)
	assert.Empty(t, byID[queued.ID].NextSteps)
}

// A plan that stopped after drafting Characters from the attached images tells, in the list of jobs, what to do with the drafts (#1022).
func TestRecent_ADraftedPlanFailureTellsWhatToDoWithTheDrafts(t *testing.T) {
	w := newWorld(t)
	p, err := w.svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "コーヒー店の動画を作りたい"})
	require.NoError(t, err)
	job := w.create(domain.JobPlan, map[string]string{"project_id": p.ID})
	require.NoError(t, w.rig.Table.MarkFailed(t.Context(), job.ID, "LLM の API キーが設定されていません。\n"+planadvice.DraftNote(1)))

	var entry joblist.Entry
	for _, e := range w.recent() {
		if e.ID == job.ID {
			entry = e
		}
	}

	assert.Contains(t, entry.Error, "登場人物の下書き")
	assert.Contains(t, entry.NextSteps, "「下書き」の印")
	assert.Contains(t, entry.NextSteps, "チャットに何かメッセージを送ると")
}

// A batch of the generation test leads to the generation test; its state and its failure are the batch's.
func TestRecent_ABatchOfTheGenerationTestLeadsToTheGenerationTest(t *testing.T) {
	w := newWorld(t)
	job := w.create(domain.JobGentest, map[string]string{"batch_id": "no-such-batch"})

	got := w.recent()
	require.Len(t, got, 1)
	assert.Equal(t, job.ID, got[0].ID)
	assert.Equal(t, "生成テスト", got[0].Label)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenGenerationTest}, got[0].Dest)
	assert.Empty(t, got[0].Subject, "a batch that is gone has no models to name")
	assert.Equal(t, joblist.Queued, got[0].State)
}

// The list reads the newest jobs and every job that has not ended: it never reads the whole table.
func TestRecent_IsBoundedButKeepsEveryJobThatHasNotEnded(t *testing.T) {
	w := newWorld(t)
	old := w.create(domain.JobModelInstall, map[string]string{"target": "engine"})
	require.NoError(t, w.rig.Table.MarkRunning(context.Background(), old.ID))
	for range joblist.Newest + 5 {
		j := w.create(domain.JobGentest, map[string]string{})
		require.NoError(t, w.rig.Table.MarkRunning(context.Background(), j.ID))
		require.NoError(t, w.rig.Table.MarkSucceeded(context.Background(), j.ID))
	}

	got := w.recent()

	assert.Len(t, got, joblist.Newest+1)
	assert.Contains(t, ids(got), old.ID)
	assert.Equal(t, old.ID, got[len(got)-1].ID, "newest first: the oldest job is last")
}

func ids(entries []joblist.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

type failingTable struct{}

func (failingTable) ListRecent(context.Context, int) ([]domain.Job, error) {
	return nil, errors.New("disk is gone")
}

func TestRecent_ReturnsTheFailureOfTheTable(t *testing.T) {
	list := joblist.New(joblist.Deps{Table: failingTable{}})
	_, err := list.Recent(t.Context())
	assert.ErrorContains(t, err, "disk is gone")
}

// An install leads to the page of the model manager that works on its target: the engine, a model, a runtime, a node, a Hugging Face file; a target that
// says nothing leads to the overview (no category).
func TestRecent_AnInstallLeadsToThePageOfItsCategory(t *testing.T) {
	w := newWorld(t)
	for target, category := range map[string]string{
		"engine":                   joblist.CategoryComfyUI,
		"model:m-a":                joblist.CategoryImageVideo,
		"runtime:gpt-sovits":       joblist.CategoryLocalRuntimes,
		"node:ipadapter-plus":      joblist.CategoryCustomNodes,
		"hf:o/m@0123456789ab/x.sf": joblist.CategoryHFExternal,
		"":                         "",
	} {
		job := w.create(domain.JobModelInstall, map[string]string{"target": target})
		var got joblist.Dest
		for _, e := range w.recent() {
			if e.ID == job.ID {
				got = e.Dest
			}
		}
		assert.Equal(t, joblist.Dest{Screen: joblist.ScreenModelManager, Category: category}, got, "target %q", target)
	}
}
