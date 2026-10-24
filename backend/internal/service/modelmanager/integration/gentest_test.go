package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// externalModels registers a ComfyUI endpoint "box" that is the fake server, with an image and a video model.
func (r *rig) externalModels(f *comfyuitest.Fake) {
	r.t.Helper()
	require.NoError(r.t, r.models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	for name, tpl := range map[string]string{"img": "img2img-sdxl", "vid": "img2video-wan22-5b", "t2i": "txt2img-sdxl"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(r.t, ok)
		require.NoError(r.t, r.models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: name, Model: tm.Model}))
	}
}

var png = comfyuitest.PNG

func TestAnImageTrialRunsOutsideAnyProjectAndIsStoredPerJob(t *testing.T) {
	r := newRig(t)
	f := comfyuitest.New(t)
	r.externalModels(f)
	var percents []int
	defer r.svc.Watch(func(e modelmanager.Event) {
		if e.Job != nil && e.Job.Update.Phase == "generate" {
			percents = append(percents, e.Job.Update.Percent)
		}
	})()

	ref, err := r.svc.StartGentest(ctx(), modelmanager.GentestInput{
		ModelID: "comfyui:box/img", Prompt: "a red fox", AspectRatio: "16:9", Reference: &modelmanager.Reference{Data: png, MimeType: "image/png"},
	})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.Equal(t, "gentest:comfyui:box/img", st.Target)
	require.NotNil(t, st.Result)
	assert.Equal(t, modelmanager.GentestSucceeded, st.Result.Status)
	assert.Greater(t, slicesMax(percents), 0, "the percentage of the run is on the job: %v", percents)

	dir := filepath.Join(r.dataDir, "gentest", ref.JobID)
	got, err := os.ReadFile(filepath.Join(dir, "output.png"))
	require.NoError(t, err)
	assert.Equal(t, png, got)
	assert.FileExists(t, filepath.Join(dir, "reference.png"), "the picture moved into the trial's directory")
	assert.FileExists(t, filepath.Join(dir, "record.json"))
	entries, _ := os.ReadDir(filepath.Join(r.dataDir, "gentest", "_inputs"))
	assert.Empty(t, entries)
	assert.Equal(t, 1, f.Count("POST /upload/image"), "the reference image reached the server")

	recs, err := r.svc.Gentests(ctx())
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, ref.JobID, recs[0].ID)
	assert.Equal(t, "a red fox", recs[0].Prompt)
	assert.True(t, recs[0].HasReference)
	assert.Equal(t, int64(len(png)), recs[0].SizeBytes)
	path, mime, err := r.svc.GentestFile(ctx(), ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "output.png"), path)
	assert.Equal(t, "image/png", mime)

	require.NoError(t, r.svc.DeleteGentest(ctx(), ref.JobID))
	assert.NoDirExists(t, dir)
	recs, _ = r.svc.Gentests(ctx())
	assert.Empty(t, recs)
	assert.ErrorIs(t, r.svc.DeleteGentest(ctx(), ref.JobID), modelmanager.ErrNotFound)
}

func TestAVideoTrialUsesTheModelsDefaultsAndTheFirstFrame(t *testing.T) {
	r := newRig(t)
	f := comfyuitest.New(t)
	r.externalModels(f)
	ref, err := r.svc.StartGentest(ctx(), modelmanager.GentestInput{ModelID: "comfyui:box/vid", Prompt: "waves", Reference: &modelmanager.Reference{Data: png}})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)
	rec := st.Result
	require.NotNil(t, rec)
	assert.Equal(t, localcfg.MediaVideo, rec.Media)
	assert.Equal(t, "480p", rec.Resolution, "the first resolution the model offers")
	assert.Equal(t, 3.0, rec.DurationSeconds)
	assert.Equal(t, "video/mp4", rec.MimeType)
	assert.Equal(t, "output.mp4", rec.File)
	assert.Equal(t, 1, f.Count("POST /upload/image"), "as the first frame")
}

func TestATrialIsCheckedAgainstTheModelBeforeItIsQueued(t *testing.T) {
	r := newRig(t)
	f := comfyuitest.New(t)
	r.externalModels(f)
	var invalid *modelmanager.InvalidError
	for name, in := range map[string]modelmanager.GentestInput{
		"no prompt":               {ModelID: "comfyui:box/t2i"},
		"bad id":                  {ModelID: "sdxl", Prompt: "x"},
		"image takes no duration": {ModelID: "comfyui:box/t2i", Prompt: "x", DurationSeconds: 3},
		"image takes no negative": {ModelID: "comfyui:box/t2i", Prompt: "x", NegativePrompt: "y"},
		"no reference images":     {ModelID: "comfyui:box/t2i", Prompt: "x", Reference: &modelmanager.Reference{Data: png}},
		"video length":            {ModelID: "comfyui:box/vid", Prompt: "x", DurationSeconds: 99},
		"video resolution":        {ModelID: "comfyui:box/vid", Prompt: "x", Resolution: "4k"},
		"aspect":                  {ModelID: "comfyui:box/t2i", Prompt: "x", AspectRatio: "wide"},
		"not an image":            {ModelID: "comfyui:box/img", Prompt: "x", Reference: &modelmanager.Reference{Data: []byte("GIF89a not what it says")}},
		"empty reference":         {ModelID: "comfyui:box/img", Prompt: "x", Reference: &modelmanager.Reference{}},
	} {
		_, err := r.svc.StartGentest(ctx(), in)
		require.ErrorAs(t, err, &invalid, name)
	}
	_, err := r.svc.StartGentest(ctx(), modelmanager.GentestInput{ModelID: "comfyui:box/missing", Prompt: "x"})
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
	assert.Zero(t, f.Count("POST /prompt"))
	jobs, err := r.table.ListByStatus(ctx(), domain.JobQueued)
	require.NoError(t, err)
	assert.Empty(t, jobs, "nothing was queued")
	entries, _ := os.ReadDir(filepath.Join(r.dataDir, "gentest", "_inputs"))
	assert.Empty(t, entries, "no picture is left behind")
}

func TestAFailedTrialKeepsItsRecordWithTheReason(t *testing.T) {
	r := newRig(t)
	f := comfyuitest.New(t)
	f.FailWith = "CUDA out of memory"
	r.externalModels(f)
	ref, err := r.svc.StartGentest(ctx(), modelmanager.GentestInput{ModelID: "comfyui:box/t2i", Prompt: "x"})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	require.Equal(t, domain.JobFailed, st.State)
	assert.Contains(t, st.Error, "CUDA out of memory")
	rec, err := r.svc.Gentest(ctx(), ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, modelmanager.GentestFailed, rec.Status)
	assert.Contains(t, rec.Error, "CUDA out of memory")
	assert.NotNil(t, rec.FinishedAt)
	_, _, err = r.svc.GentestFile(ctx(), ref.JobID)
	assert.ErrorIs(t, err, modelmanager.ErrNotFound, "no result file")
	assert.NoError(t, r.svc.DeleteGentest(ctx(), ref.JobID))
}

// A trial is not resumable: a half-done ComfyUI prompt has no owner after a restart, so it is failed.
func TestARunningTrialIsFailedWhenMamStops(t *testing.T) {
	r := newRig(t)
	f := comfyuitest.New(t)
	f.Hold = make(chan struct{})
	r.externalModels(f)
	ref, err := r.svc.StartGentest(ctx(), modelmanager.GentestInput{ModelID: "comfyui:box/t2i", Prompt: "x"})
	require.NoError(t, err)
	r.start()
	require.Eventually(t, func() bool { return f.Count("POST /prompt") == 1 }, 20*time.Second, 10*time.Millisecond)
	rec, err := r.svc.Gentest(ctx(), ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, modelmanager.GentestRunning, rec.Status)
	assert.ErrorIs(t, r.svc.DeleteGentest(ctx(), ref.JobID), modelmanager.ErrBusy)

	r.queue.Stop()
	st, err := r.svc.JobStatus(ctx(), ref.JobID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobFailed, st.State)
	assert.Equal(t, jobqueue.ShutdownReason, st.Error)
	rec, _ = r.svc.Gentest(ctx(), ref.JobID)
	assert.Equal(t, modelmanager.GentestFailed, rec.Status)
}

// A record left "running" by a crash (the job was failed by the next start) is shown as failed.
func TestARecordLeftRunningByACrashShowsAsFailed(t *testing.T) {
	r := newRig(t)
	job, err := r.table.Create(ctx(), domain.JobGentest, json.RawMessage(`{"model_id":"comfyui:box/t2i"}`))
	require.NoError(t, err)
	dir := filepath.Join(r.dataDir, "gentest", job.ID)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	data, _ := json.Marshal(modelmanager.GentestRecord{ID: job.ID, Status: modelmanager.GentestRunning, CreatedAt: time.Now()})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "record.json"), data, 0o600))
	require.NoError(t, r.table.MarkFailed(ctx(), job.ID, jobqueue.InterruptedReason))
	rec, err := r.svc.Gentest(ctx(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, modelmanager.GentestFailed, rec.Status)
	assert.Equal(t, jobqueue.InterruptedReason, rec.Error)
	assert.NoError(t, r.svc.DeleteGentest(ctx(), job.ID))
}

// A trial on the bundled endpoint starts the engine first. The fake engine answers /system_stats only, so the run
// itself fails, but the engine is up and the endpoint points at it.
func TestATrialOnTheBundledEndpointStartsTheEngine(t *testing.T) {
	r := newRig(t)
	r.installEngine()
	require.Equal(t, domain.JobSucceeded, r.installModel("m-a").State)
	assert.Equal(t, proc.StateStopped, r.svc.Engine().Process.State)
	ref, err := r.svc.StartGentest(ctx(), modelmanager.GentestInput{ModelID: "comfyui:bundled-comfyui/wf-a", Prompt: "x"})
	require.NoError(t, err)
	st := r.wait(ref.JobID)
	assert.Equal(t, domain.JobFailed, st.State, "the fake engine cannot generate")
	assert.Equal(t, proc.StateRunning, r.svc.Engine().Process.State)
}

func TestGentestIDsAreNotPaths(t *testing.T) {
	r := newRig(t)
	for _, id := range []string{"../x", "a/b", "", "..", "x"} {
		_, err := r.svc.Gentest(ctx(), id)
		assert.ErrorIs(t, err, modelmanager.ErrNotFound, id)
		_, _, err = r.svc.GentestFile(ctx(), id)
		assert.ErrorIs(t, err, modelmanager.ErrNotFound, id)
		assert.ErrorIs(t, r.svc.DeleteGentest(ctx(), id), modelmanager.ErrNotFound, id)
	}
	recs, err := r.svc.Gentests(ctx())
	require.NoError(t, err)
	assert.Empty(t, recs)
}

// The job kinds are registered with the right resumability: after an abnormal stop an install is run again, a
// trial is failed.
func TestRecoverResumesAnInstallAndFailsATrial(t *testing.T) {
	r := newRig(t)
	install, err := r.table.Create(ctx(), domain.JobModelInstall, json.RawMessage(`{"target":"model:m-a"}`))
	require.NoError(t, err)
	trial, err := r.table.Create(ctx(), domain.JobGentest, json.RawMessage(`{"model_id":"comfyui:box/t2i"}`))
	require.NoError(t, err)
	for _, j := range []string{install.ID, trial.ID} {
		require.NoError(t, r.table.MarkRunning(ctx(), j))
	}
	resumed, failed, err := r.queue.Recover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, resumed)
	assert.Equal(t, 1, failed)
	st := r.wait(install.ID)
	assert.Equal(t, domain.JobSucceeded, st.State, st.Error)
	assert.True(t, r.exists(ckptA), "the interrupted install finished")
	st, err = r.svc.JobStatus(ctx(), trial.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobFailed, st.State)
}

func slicesMax(v []int) int { return slices.Max(append([]int{0}, v...)) }

func TestJobStatusKnowsOnlyItsKinds(t *testing.T) {
	r := newRig(t)
	other, err := r.table.Create(ctx(), domain.JobPlan, json.RawMessage(`{}`))
	require.NoError(t, err)
	_, err = r.svc.JobStatus(ctx(), other.ID)
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
	_, err = r.svc.JobStatus(ctx(), "no-such-job")
	assert.ErrorIs(t, err, modelmanager.ErrNotFound)
}
