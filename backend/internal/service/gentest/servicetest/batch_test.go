package servicetest_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

func countRows(t *testing.T, r *gentesttest.Rig, table string) int {
	t.Helper()
	var n int
	require.NoError(t, r.DB.Reader().QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

func TestBatchRunsEveryCombinationAndRecordsEachResult(t *testing.T) {
	r := gentesttest.New(t)
	req := gentest.Request{
		Media: "image", Models: []string{"flux-2", "nano-banana"}, Count: 2, AspectRatio: "16:9",
		Prompts: []gentest.PromptInput{{Prompt: "a fox", Negative: "blurry"}, {Prompt: "a cat"}},
	}
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	assert.Equal(t, 8, ref.TotalItems)
	st := r.Wait(ref.BatchID)

	assert.Equal(t, gentest.StateSucceeded, st.State)
	assert.Equal(t, 8, st.Total)
	assert.Equal(t, 8, st.Succeeded)
	require.Len(t, st.Results, 8)
	combos := map[string]int{}
	for _, res := range st.Results {
		combos[res.Model+"|"+res.Prompt]++
		assert.Equal(t, gentestrepo.StatusSucceeded, res.Status)
		assert.True(t, res.HasFile)
		assert.Equal(t, "16:9", res.AspectRatio)
	}
	assert.Equal(t, map[string]int{"flux-2|a fox": 2, "flux-2|a cat": 2, "nano-banana|a fox": 2, "nano-banana|a cat": 2}, combos, "every model x prompt x count")
	assert.Equal(t, "flux-2", st.Results[0].Model, "planned order: model, prompt, count")
	assert.Equal(t, "a fox", st.Results[0].Prompt)

	// The files are in the gentest directory, outside the assets.
	for _, res := range st.Results {
		f, err := r.Svc.ResultFile(context.Background(), res.ID)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(f.Path))
		rel, _ := filepath.Rel(filepath.Join(r.DataDir, "gentest"), f.Path)
		assert.NotContains(t, rel, "..")
		data, err := os.ReadFile(f.Path)
		require.NoError(t, err)
		assert.Equal(t, gentesttest.PNG, data)
	}
	entries, _ := os.ReadDir(r.AssetsDir)
	assert.Empty(t, entries, "nothing is written to the assets directory")
}

func TestBatchCallsTheNamedModelWithThePromptAsTypedAndTheNegativeFoldedWhereThereIsNoField(t *testing.T) {
	r := gentesttest.New(t)
	ref, err := r.Confirmed(gentest.Request{
		Media: "video", Models: []string{"kling-3.0", "seedance-2.0"}, Prompts: []gentest.PromptInput{{Prompt: "waves at dusk", Negative: "text, logo"}},
		Resolution: "720p", DurationSeconds: 5,
	})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, gentest.StateSucceeded, st.State)

	kling := r.Fake.CallsOf(domain.ModelKling3)
	require.Len(t, kling, 1)
	assert.Equal(t, "waves at dusk", kling[0].Video.Prompt, "the prompt goes as typed")
	assert.Equal(t, "text, logo", kling[0].Video.NegativePrompt, "Kling has the field")
	seedance := r.Fake.CallsOf(domain.ModelSeedance2)
	require.Len(t, seedance, 1)
	assert.Equal(t, "waves at dusk Avoid: text, logo.", seedance[0].Video.Prompt, "no field: folded as the Shot prompts do")
	assert.Empty(t, seedance[0].Video.NegativePrompt)

	modes := map[string]string{}
	sent := map[string]string{}
	for _, res := range st.Results {
		modes[res.Model], sent[res.Model] = res.NegativeMode, res.SentPrompt
		assert.Equal(t, "waves at dusk", res.Prompt, "the typed prompt is kept beside what was sent")
		assert.Equal(t, "text, logo", res.NegativePrompt)
	}
	assert.Equal(t, map[string]string{"kling-3.0": "field", "seedance-2.0": "folded"}, modes)
	assert.Equal(t, "waves at dusk Avoid: text, logo.", sent["seedance-2.0"])
	assert.Equal(t, "waves at dusk", sent["kling-3.0"])
}

func TestBatchNeverFallsBackToAnotherModel(t *testing.T) {
	r := gentesttest.New(t)
	r.Fake.FailWith(domain.ModelFlux2, &modeladapter.ProviderError{Vendor: "flux", Status: http.StatusUnprocessableEntity, Message: "refused"})
	ref, err := r.Confirmed(imageReq("flux-2", "nano-banana"))
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)

	require.Len(t, st.Results, 2)
	assert.Equal(t, gentestrepo.StatusFailed, st.Results[0].Status)
	assert.Contains(t, st.Results[0].Error, "refused")
	assert.Equal(t, gentestrepo.StatusSucceeded, st.Results[1].Status, "the other model is unaffected")
	assert.Len(t, r.Fake.CallsOf(domain.ModelFlux2), 1, "a 422 is not retried")
	assert.Len(t, r.Fake.CallsOf(domain.ModelNanoBanana), 1, "Nano Banana is not the stand-in for FLUX.2: it was asked for its own result only")
	assert.Equal(t, gentest.StateSucceeded, st.State, "a batch with a file succeeded as a batch")
}

func TestSeedIsGeneratedPerResultAndRecordedOnlyWhereItIsSent(t *testing.T) {
	r := gentesttest.New(t)
	req := imageReq("flux-2", "nano-banana")
	req.Count = 3
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)

	var fluxSeeds []int64
	for _, res := range st.Results {
		switch res.Model {
		case "flux-2":
			require.NotNil(t, res.Seed)
			assert.True(t, res.SeedSent)
			fluxSeeds = append(fluxSeeds, *res.Seed)
		default:
			assert.Nil(t, res.Seed, "no seed field: NULL, not 0")
			assert.False(t, res.SeedSent)
		}
	}
	assert.Equal(t, []int64{1001, 1002, 1003}, fluxSeeds, "n results, n different seeds")
	sent := []int64{}
	for _, c := range r.Fake.CallsOf(domain.ModelFlux2) {
		require.NotNil(t, c.Image.Seed)
		sent = append(sent, *c.Image.Seed)
	}
	assert.ElementsMatch(t, fluxSeeds, sent, "the seed that is stored is the one that was sent")
	for _, c := range r.Fake.CallsOf(domain.ModelNanoBanana) {
		assert.Nil(t, c.Image.Seed)
	}
	assert.Len(t, r.Fake.CallsOf(domain.ModelNanoBanana), 3, "n calls with identical input")
}

func TestRealRandomSeedFitsRunway(t *testing.T) {
	r := gentesttest.New(t)
	svc := gentest.New(gentest.Deps{Repo: r.Repo, Adapter: r.Adapter, Jobs: r.Queue, JobTable: r.Jobs, Projects: nil, DataDir: r.DataDir})
	seen := map[int64]bool{}
	for range 50 {
		seen[svc.Seed()] = true
	}
	for s := range seen {
		assert.True(t, s >= 0 && s <= 4294967295, "%d", s)
	}
	assert.Greater(t, len(seen), 40)
}

func TestBatchRecordsNoTakeLedgerOrAsset(t *testing.T) {
	r := gentesttest.New(t)
	ref, err := r.Confirmed(imageReq("flux-2", "nano-banana"))
	require.NoError(t, err)
	r.Wait(ref.BatchID)

	for _, table := range []string{"take", "cost_ledger_entry", "asset", "reference_item"} {
		assert.Zero(t, countRows(t, r, table), table)
	}
	assert.Equal(t, 2, countRows(t, r, "gentest_result"))
}

func TestBatchStoresInputsAndGivesEachModelOnlyWhatItUses(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithNoReferenceModel())
	id, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	req := imageReq(string(gentesttest.NoReferenceModel), "nano-banana")
	req.References = []gentest.RefInput{{Source: gentest.FromStaged, ID: id}}
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)

	require.Equal(t, gentest.StateSucceeded, st.State)
	assert.Empty(t, r.Fake.CallsOf(gentesttest.NoReferenceModel)[0].Image.Inputs, "a model without image inputs gets none")
	assert.Len(t, r.Fake.CallsOf(domain.ModelNanoBanana)[0].Image.Inputs, 1)
	assert.Empty(t, st.Results[0].UsedInputs)
	assert.Len(t, st.Results[1].UsedInputs, 1)

	d, err := r.Svc.Result(context.Background(), st.Results[1].ID)
	require.NoError(t, err)
	require.Len(t, d.Inputs, 1, "the input is part of the record")
	assert.Equal(t, st.Results[1].UsedInputs[0], d.Inputs[0].ID)
	assert.Equal(t, "upload", d.Inputs[0].Origin.Kind)
	f, err := r.Svc.InputFile(context.Background(), d.Inputs[0].ID)
	require.NoError(t, err)
	copied, err := os.ReadFile(f.Path)
	require.NoError(t, err)
	assert.Equal(t, gentesttest.PNG, copied, "the input is copied into the gentest directory")
	_, _, err = r.Svc.StagedFile(id)
	assert.ErrorIs(t, err, gterr.ErrNotFound, "the staged upload is consumed")
}

func TestVideoRolesAreMappedToTheRequest(t *testing.T) {
	r := gentesttest.New(t)
	first, _, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	last, _, err := r.Svc.StageReference(append([]byte(nil), gentesttest.PNG...))
	require.NoError(t, err)
	ref, err := r.Confirmed(gentest.Request{
		Media: "video", Models: []string{"kling-3.0", "runway-gen-4.5"}, Prompts: []gentest.PromptInput{p("x")}, Resolution: "720p",
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: first, Role: "first_frame"}, {Source: gentest.FromStaged, ID: last, Role: "last_frame"}},
	})
	require.NoError(t, err)
	r.Wait(ref.BatchID)

	k := r.Fake.CallsOf(domain.ModelKling3)[0].Video
	assert.NotNil(t, k.FirstFrame)
	assert.NotNil(t, k.LastFrame)
	rw := r.Fake.CallsOf(domain.ModelRunwayGen45)[0].Video
	assert.NotNil(t, rw.FirstFrame)
	assert.Nil(t, rw.LastFrame, "Runway has no last frame")
}
