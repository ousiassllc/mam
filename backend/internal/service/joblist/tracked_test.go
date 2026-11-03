package joblist_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobrecent"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/worktrack"
)

// tracked is a world whose list also reads a tracker of the works that have no row in the job table (what the editor, the workflows,
// Codex and the ffmpeg download keep in memory). The clock is fixed, so the time a work has run is exact.
type tracked struct {
	*world
	works  *worktrack.Tracker
	now    time.Time
	hidden map[string]bool
}

func newTracked(t *testing.T) *tracked {
	t.Helper()
	w := newWorld(t)
	tr := &tracked{world: w, works: &worktrack.Tracker{}, now: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), hidden: map[string]bool{}}
	tr.works.Now = func() time.Time { return tr.now }
	w.list = joblist.New(joblist.Deps{
		Table: jobrecent.New(w.rig.DB()), Shots: repository.NewGenerationRepository(w.db), Projects: w.svc.Projects, ModelManager: w.rig.Svc, Gentest: w.rig.Gentest,
		Works: []worktrack.Source{tr.works}, Hidden: func(id string) bool { return tr.hidden[id] }, Now: func() time.Time { return tr.now },
	})
	return tr
}

func (tr *tracked) byKind() map[string]joblist.Entry {
	tr.t.Helper()
	out := map[string]joblist.Entry{}
	for _, e := range tr.recent() {
		out[e.Kind] = e
	}
	return out
}

// Every kind of work leads to the screen it is worked on in and names what it is on. A Project that is gone leaves the name and the link
// out, as it does for a Shot; a workflow of a hidden model (an adult one while the age confirmation is off) is not named (#992).
func TestRecent_TrackedWorksNameWhatTheyAreOnAndLeadToTheirScreen(t *testing.T) {
	tr := newTracked(t)
	p, err := tr.svc.Projects.Create(t.Context(), service.CreateProjectInput{Content: "コーヒー店の動画を作りたい"})
	require.NoError(t, err)
	for _, kind := range []worktrack.Kind{worktrack.KindExport, worktrack.KindAudio, worktrack.KindEditorJudge} {
		tr.works.Begin(worktrack.Record{Kind: kind, ProjectID: p.ID}, "working")
	}
	tr.works.Begin(worktrack.Record{Kind: worktrack.KindWorkflowRun, Subject: "Stills", ModelID: "comfy:stills@v1"}, "ComfyUI を準備しています")
	tr.works.Begin(worktrack.Record{Kind: worktrack.KindCodexInstall, Subject: "Codex"}, "Codex をダウンロードしています")
	tr.works.Begin(worktrack.Record{Kind: worktrack.KindFFmpegInstall, Subject: "ffmpeg"}, "ffmpeg をダウンロードしています")

	got := tr.byKind()
	require.Len(t, got, 6)
	editor := joblist.Dest{Screen: joblist.ScreenProjectEditor, ProjectID: p.ID}
	for kind, label := range map[worktrack.Kind]string{worktrack.KindExport: "動画の書き出し", worktrack.KindAudio: "音声の作成", worktrack.KindEditorJudge: "Editor の判定"} {
		e := got[string(kind)]
		assert.Equal(t, label, e.Label)
		assert.Equal(t, p.Name, e.Subject)
		assert.Equal(t, editor, e.Dest)
		assert.Equal(t, joblist.Running, e.State)
	}
	assert.Equal(t, "ワークフローの実行", got["workflow_run"].Label)
	assert.Equal(t, "Stills", got["workflow_run"].Subject)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenWorkflows}, got["workflow_run"].Dest)
	assert.Equal(t, "Codex の導入", got["codex_install"].Label)
	assert.Equal(t, "Codex", got["codex_install"].Subject)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenChatGPT}, got["codex_install"].Dest)
	assert.Equal(t, "ffmpeg のダウンロード", got["ffmpeg_install"].Label)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenFFmpeg}, got["ffmpeg_install"].Dest)

	tr.hidden["comfy:stills@v1"] = true
	assert.Empty(t, tr.byKind()["workflow_run"].Subject, "a hidden model's workflow is not named")
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenWorkflows}, tr.byKind()["workflow_run"].Dest)

	orphan := newTracked(t)
	orphan.works.Begin(worktrack.Record{Kind: worktrack.KindExport, ProjectID: "no-such-project"}, "working")
	gone := orphan.byKind()["export"]
	assert.Empty(t, gone.Subject, "the Project is gone")
	assert.Equal(t, joblist.Dest{}, gone.Dest)
}

// A training of a Character's voice is named by the Character and leads to the screen of its owner: the Project's World State or the Series page.
func TestRecent_AVoiceTrainingIsNamedByTheCharacterAndLeadsToItsOwner(t *testing.T) {
	tr := newTracked(t)
	tr.works.Begin(worktrack.Record{Kind: worktrack.KindVoiceTrain, ProjectID: "p-1", Subject: "ユイ"}, "SoVITS の学習 エポック 3 / 8")
	tr.now = tr.now.Add(time.Minute)
	tr.works.Begin(worktrack.Record{Kind: worktrack.KindVoiceTrain, SeriesID: "s-1", Subject: "ハル"}, "音声の特徴（SSL） 12 / 30 件")

	var got []joblist.Entry
	for _, e := range tr.recent() {
		if e.Kind == "voice_train" {
			got = append(got, e)
		}
	}

	require.Len(t, got, 2)
	assert.Equal(t, "声の学習", got[0].Label)
	assert.Equal(t, "ハル", got[0].Subject, "the newest first")
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenSeries, SeriesID: "s-1"}, got[0].Dest)
	assert.Equal(t, "ユイ", got[1].Subject)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenProjectWorld, ProjectID: "p-1"}, got[1].Dest)
	assert.Equal(t, joblist.Running, got[1].State)
	require.NotNil(t, got[1].Progress)
	assert.Contains(t, got[1].Progress.Text, "SoVITS の学習 エポック 3 / 8")
}

// A work goes from running to its end like a row of the table does: while it runs it says what it does (and how far it is when it knows,
// otherwise how long it has run, so that a long render is seen to be going on); when it ends the progress is gone, and a failure carries
// its reason with the secrets hidden.
func TestRecent_ATrackedWorkGoesFromRunningToItsEnd(t *testing.T) {
	tr := newTracked(t)
	render := tr.works.Begin(worktrack.Record{Kind: worktrack.KindExport}, "動画を書き出しています")
	download := tr.works.Begin(worktrack.Record{Kind: worktrack.KindFFmpegInstall, Subject: "ffmpeg"}, "ffmpeg をダウンロードしています")
	download.Progress(40, "ffmpeg をダウンロードしています")
	tr.now = tr.now.Add(3*time.Minute + 20*time.Second)

	got := tr.byKind()
	r := got["export"]
	assert.Equal(t, joblist.Running, r.State)
	require.NotNil(t, r.Progress)
	assert.Equal(t, joblist.NoPercent, r.Progress.Percent, "a render cannot say how far it is")
	assert.Equal(t, "動画を書き出しています・経過 3分20秒", r.Progress.Text)
	assert.Nil(t, r.FinishedAt)
	d := got["ffmpeg_install"]
	require.NotNil(t, d.Progress)
	assert.Equal(t, joblist.Progress{Percent: 40, Text: "ffmpeg をダウンロードしています"}, *d.Progress)

	render.Succeed()
	download.Fail("download failed: key sk-proj-abcdefghijklmnopqrstuvwxyz0123 was refused")
	got = tr.byKind()
	assert.Equal(t, joblist.Succeeded, got["export"].State)
	assert.Nil(t, got["export"].Progress, "a work that ended has no progress")
	require.NotNil(t, got["export"].FinishedAt)
	assert.Equal(t, joblist.Failed, got["ffmpeg_install"].State)
	assert.Contains(t, got["ffmpeg_install"].Error, "download failed")
	assert.NotContains(t, got["ffmpeg_install"].Error, "sk-proj-abcdefghijklmnopqrstuvwxyz0123")
	assert.Nil(t, got["ffmpeg_install"].Progress)

	cancelled := tr.works.Begin(worktrack.Record{Kind: worktrack.KindCodexInstall, Subject: "Codex"}, "")
	cancelled.Cancel()
	assert.Equal(t, joblist.Cancelled, tr.byKind()["codex_install"].State)
	assert.Empty(t, tr.byKind()["codex_install"].Error)
}

// The works and the rows of the table are one list, the newest start first, and a work of the list ends up in the same Row shape (id,
// label, state, start) as a row of the table.
func TestRecent_TrackedWorksAreMergedWithTheRowsOfTheTableNewestFirst(t *testing.T) {
	tr := newTracked(t)
	old := tr.create(domain.JobGentest, map[string]string{})
	require.NoError(t, tr.rig.Table.MarkRunning(context.Background(), old.ID))
	tr.now = time.Now().Add(time.Minute) // the work starts after the row, whatever the clock of the table
	tr.works.Begin(worktrack.Record{Kind: worktrack.KindFFmpegInstall, Subject: "ffmpeg"}, "")

	got := tr.recent()
	require.Len(t, got, 2)
	assert.Equal(t, "ffmpeg_install", got[0].Kind, "the work started after the row")
	assert.Equal(t, string(domain.JobGentest), got[1].Kind)
	assert.NotEmpty(t, got[0].ID)
	assert.False(t, got[0].StartedAt.IsZero())
}
