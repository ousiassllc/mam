package joblist_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Equal(t, "生成テスト", running[0].Label)
	assert.Equal(t, joblist.Running, running[0].State)
	assert.Equal(t, joblist.Dest{Screen: joblist.ScreenGenerationTest}, running[0].Dest)
	assert.NotEmpty(t, running[0].Subject, "the model of the batch")
	require.NotNil(t, running[0].Progress)
	assert.Equal(t, "0 / 2 件", running[0].Progress.Text)

	release <- struct{}{}
	release <- struct{}{}
	rig.Wait(ref.BatchID)
	done, err := list.Recent(t.Context())
	require.NoError(t, err)
	require.Len(t, done, 1)
	assert.Equal(t, joblist.Succeeded, done[0].State)
	assert.NotNil(t, done[0].FinishedAt)
}
