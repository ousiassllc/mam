package joblist_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui/live"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobrecent"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

// A batch of the generation test is listed with the models it runs, how many of its results are over and the state of the batch;
// it ends as succeeded with no progress.
func TestRecent_ABatchNamesItsModelsAndCountsItsResults(t *testing.T) {
	rig := gentesttest.New(t)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	list := joblist.New(joblist.Deps{
		Table: jobrecent.New(rig.DB), Shots: repository.NewGenerationRepository(rig.DB), Projects: svc.Projects, Gentest: rig.Svc,
	})
	release := rig.Fake.Hold("flux-2")
	ref, err := rig.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}, {Prompt: "a cat"}}})
	require.NoError(t, err)
	<-rig.Fake.Started()

	running, err := list.Recent(t.Context())
	require.NoError(t, err)
	require.Len(t, running, 1)
	assert.Equal(t, "単発生成", running[0].Label)
	assert.Equal(t, joblist.Running, running[0].State)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenGenerationTest}, running[0].Dest)
	assert.NotEmpty(t, running[0].Subject, "the model of the batch")
	require.NotNil(t, running[0].Progress)
	assert.Equal(t, "0 / 2 件", running[0].Progress.Text)

	release <- struct{}{}
	release <- struct{}{}
	rig.Wait(ref.BatchID)
	// The batch is over when its results are; the job row gets its end time a moment later, when the runner returns to the queue.
	var done []joblist.Entry
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var err error
		done, err = list.Recent(t.Context())
		require.NoError(c, err)
		require.Len(c, done, 1)
		assert.NotNil(c, done[0].FinishedAt)
	}, 20*time.Second, 10*time.Millisecond, "the job did not get its end time")
	assert.Equal(t, joblist.Succeeded, done[0].State)
}

// The age gate closed (Issue #992): a batch of the generation test does not name a model that is hidden (as the install of a catalog model does not);
// the models that are not hidden stay in its subject.
func TestRecent_ABatchDoesNotNameAHiddenModel(t *testing.T) {
	hidden := false
	rig := gentesttest.New(t, gentesttest.WithHidden(func(id string) bool { return hidden && id == "flux-2" }))
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	list := joblist.New(joblist.Deps{
		Table: jobrecent.New(rig.DB), Shots: repository.NewGenerationRepository(rig.DB), Projects: svc.Projects, Gentest: rig.Svc,
	})
	ref, err := rig.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2", "nano-banana"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}}})
	require.NoError(t, err)
	batch := rig.Wait(ref.BatchID)
	require.Len(t, batch.Results, 2)
	labels := map[string]string{}
	for _, res := range batch.Results {
		labels[res.Model] = res.ModelLabel
	}
	require.NotEmpty(t, labels["flux-2"])
	require.NotEmpty(t, labels["nano-banana"])

	open, err := list.Recent(t.Context())
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Contains(t, open[0].Subject, labels["flux-2"], "the gate is open: both models are named")
	assert.Contains(t, open[0].Subject, labels["nano-banana"])

	hidden = true
	closed, err := list.Recent(t.Context())
	require.NoError(t, err)
	require.Len(t, closed, 1)
	assert.NotContains(t, closed[0].Subject, labels["flux-2"])
	assert.Equal(t, labels["nano-banana"], closed[0].Subject)
}

// A batch whose ComfyUI call reports a step shows the node, the step and the time since the call was queued next to the count, and
// the percentage counts the step (Issue #1002).
func TestRecent_ARunningBatchShowsTheStepOfItsComfyUICall(t *testing.T) {
	rig := gentesttest.New(t)
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	list := joblist.New(joblist.Deps{
		Table: jobrecent.New(rig.DB), Shots: repository.NewGenerationRepository(rig.DB), Projects: svc.Projects, Gentest: rig.Svc,
	})
	release := rig.Fake.Hold("flux-2")
	ref, err := rig.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}, {Prompt: "a cat"}}})
	require.NoError(t, err)
	<-rig.Fake.Started()

	call := live.Default.Start("test-ep", "prompt-"+ref.JobID)
	defer call.End()
	call.Own(ref.JobID)
	call.Step("3", "KSampler", 5, 20, 50)

	running, err := list.Recent(t.Context())
	require.NoError(t, err)
	require.Len(t, running, 1)
	require.NotNil(t, running[0].Progress)
	assert.Equal(t, 25, running[0].Progress.Percent, "0 of 2 done and the running one half way")
	assert.Contains(t, running[0].Progress.Text, "0 / 2 件・KSampler ステップ 5 / 20・経過 ")

	call.End()
	release <- struct{}{}
	release <- struct{}{}
	rig.Wait(ref.BatchID)
}
