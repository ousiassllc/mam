package servicetest_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

func runImages(t *testing.T, r *gentesttest.Rig, models ...string) gentest.BatchStatus {
	t.Helper()
	ref, err := r.Confirmed(imageReq(models...))
	require.NoError(t, err)
	return r.Wait(ref.BatchID)
}

func TestHistoryListsNewestFirstFiltersAndTotalsTheCost(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	first := runImages(t, r, "flux-2")
	vref, err := r.Confirmed(gentest.Request{Media: "video", Models: []string{"runway-gen-4.5"}, Prompts: []gentest.PromptInput{p("waves")}, Resolution: "720p", DurationSeconds: 4})
	require.NoError(t, err)
	video := r.Wait(vref.BatchID)
	third := runImages(t, r, "nano-banana")
	require.NoError(t, r.Svc.SetFavorite(ctx, first.Results[0].ID, true))

	all, err := r.Svc.History(ctx, gentest.HistoryFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, third.Results[0].ID, all[0].ID, "newest first")
	assert.Equal(t, first.Results[0].ID, all[2].ID)

	vids, _ := r.Svc.History(ctx, gentest.HistoryFilter{Media: "video"})
	require.Len(t, vids, 1)
	assert.Equal(t, video.Results[0].ID, vids[0].ID)
	assert.Equal(t, 4.0, vids[0].DurationSeconds)
	byModel, _ := r.Svc.History(ctx, gentest.HistoryFilter{Model: "nano-banana"})
	assert.Len(t, byModel, 1)
	fav, _ := r.Svc.History(ctx, gentest.HistoryFilter{FavoriteOnly: true})
	require.Len(t, fav, 1)
	assert.True(t, fav[0].Favorite)
	_, err = r.Svc.History(ctx, gentest.HistoryFilter{Media: "audio"})
	assert.Error(t, err)

	models, _ := r.Svc.HistoryModels(ctx)
	assert.Equal(t, []string{"flux-2", "nano-banana", "runway-gen-4.5"}, models)
	spend, err := r.Svc.Spend(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, spend.Items)
	assert.InDelta(t, 0.03+0.0336+0.12*4, spend.PricedUSD, 1e-9)

	require.NoError(t, r.Svc.SetFavorite(ctx, first.Results[0].ID, false))
	fav, _ = r.Svc.History(ctx, gentest.HistoryFilter{FavoriteOnly: true})
	assert.Empty(t, fav)
	assert.ErrorIs(t, r.Svc.SetFavorite(ctx, "nope", true), gterr.ErrNotFound)
}

func TestDetailHasTheSettingsOfTheResult(t *testing.T) {
	r := gentesttest.New(t)
	st := runImages(t, r, "flux-2")
	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)

	assert.Equal(t, "flux-2", d.Model)
	assert.Equal(t, "flux-2-pro", d.APIModel)
	assert.Equal(t, "a fox", d.Prompt)
	assert.Equal(t, "1:1", d.AspectRatio)
	require.NotNil(t, d.Seed)
	assert.NotNil(t, d.EstimatedUSD)
	assert.Nil(t, d.ActualUSD, "the vendor reported no cost")
	assert.InDelta(t, 0.03, d.BatchEstimatedUSD, 1e-9)
	assert.Equal(t, "priced", d.CostBasis)
	assert.Empty(t, d.Imports)
	_, err = r.Svc.Result(context.Background(), "nope")
	assert.ErrorIs(t, err, gterr.ErrNotFound)
}

func TestDeleteResultRemovesItsFileAndTheLastOneTakesTheBatchDirectory(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	req := imageReq("flux-2")
	req.Count = 2
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	f0, _ := r.Svc.ResultFile(ctx, st.Results[0].ID)
	f1, _ := r.Svc.ResultFile(ctx, st.Results[1].ID)
	dir := filepath.Dir(f0.Path)

	require.NoError(t, r.Svc.DeleteResult(ctx, st.Results[0].ID))
	assert.NoFileExists(t, f0.Path)
	assert.FileExists(t, f1.Path)
	_, err = r.Svc.ResultFile(ctx, st.Results[0].ID)
	assert.ErrorIs(t, err, gterr.ErrNotFound)

	require.NoError(t, r.Svc.DeleteResult(ctx, st.Results[1].ID))
	assert.NoDirExists(t, dir)
	_, err = r.Svc.Batch(ctx, ref.BatchID, false)
	assert.ErrorIs(t, err, gterr.ErrNotFound)
	assert.ErrorIs(t, r.Svc.DeleteResult(ctx, st.Results[1].ID), gterr.ErrNotFound)

	prompts, _ := r.Svc.Prompts(ctx)
	assert.Len(t, prompts, 1, "the prompt history does not depend on the results")
}

func TestDeleteRefusesWhatIsStillRunning(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	release := r.Fake.Hold(domain.ModelFlux2)
	ref, err := r.Confirmed(imageReq("flux-2"))
	require.NoError(t, err)
	<-r.Fake.Started()
	st, _ := r.Svc.Batch(ctx, ref.BatchID, true)

	var c *gterr.ConflictError
	assert.ErrorAs(t, r.Svc.DeleteResult(ctx, st.Results[0].ID), &c)
	assert.ErrorAs(t, r.Svc.DeleteBatch(ctx, ref.BatchID), &c)
	release <- struct{}{}
	r.Wait(ref.BatchID)
	require.NoError(t, r.Svc.DeleteBatch(ctx, ref.BatchID))
	assert.NoDirExists(t, filepath.Join(r.DataDir, "gentest", ref.BatchID))
}

func TestPromptHistoryDedupesAndIsDeletedOnlyOnPurpose(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	runImages(t, r, "flux-2")
	runImages(t, r, "nano-banana")
	ref, err := r.Confirmed(gentest.Request{Media: "image", Models: []string{"flux-2"}, Prompts: []gentest.PromptInput{{Prompt: "a fox", Negative: "blur"}, p("a bird")}})
	require.NoError(t, err)
	r.Wait(ref.BatchID)

	list, err := r.Svc.Prompts(ctx)
	require.NoError(t, err)
	require.Len(t, list, 3, "(a fox, ''), (a fox, blur), (a bird)")
	counts := map[string]int{}
	for _, x := range list {
		counts[x.Prompt+"|"+x.NegativePrompt] = x.UseCount
	}
	assert.Equal(t, map[string]int{"a fox|": 2, "a fox|blur": 1, "a bird|": 1}, counts)

	one, err := r.Svc.Prompt(ctx, list[0].ID)
	require.NoError(t, err)
	assert.Equal(t, list[0].Prompt, one.Prompt)
	require.NoError(t, r.Svc.DeletePrompt(ctx, list[0].ID))
	assert.ErrorIs(t, r.Svc.DeletePrompt(ctx, list[0].ID), gterr.ErrNotFound)
	list2, _ := r.Svc.Prompts(ctx)
	assert.Len(t, list2, 2)
}

func TestInputsFromAProjectAndFromTheHistory(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	project := r.NewProject()
	assetID := domain.NewID()
	rel := filepath.Join(project, assetID+".png")
	require.NoError(t, os.MkdirAll(filepath.Join(r.AssetsDir, project), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(r.AssetsDir, rel), gentesttest.PNG, 0o600))
	itemID := domain.NewID()
	require.NoError(t, r.SeedAsset(ctx, project, assetID, itemID, rel))

	images, err := r.Svc.ProjectImages(ctx, project)
	require.NoError(t, err)
	require.Len(t, images, 1)
	assert.Equal(t, itemID, images[0].ReferenceItemID)
	assert.Equal(t, "Mio", images[0].Name)
	_, err = r.Svc.ProjectImages(ctx, "nope")
	assert.ErrorIs(t, err, gterr.ErrNotFound)
	projects, _ := r.Svc.ProjectList(ctx)
	require.Len(t, projects, 1)

	hist := runImages(t, r, "flux-2")
	for _, ref := range []gentest.RefInput{
		{Source: gentest.FromProjectAsset, ProjectID: project, ID: assetID},
		{Source: gentest.FromReferenceItem, ProjectID: project, ID: itemID},
		{Source: gentest.FromHistory, ID: hist.Results[0].ID},
	} {
		req := imageReq("nano-banana")
		req.References = []gentest.RefInput{ref}
		run, err := r.Confirmed(req)
		require.NoError(t, err, ref.Source)
		st := r.Wait(run.BatchID)
		require.Equal(t, gentest.StateSucceeded, st.State, ref.Source)
		d, err := r.Svc.Result(ctx, st.Results[0].ID)
		require.NoError(t, err)
		assert.Equal(t, ref.Source, d.Inputs[0].Origin.Kind, "the origin is recorded")
	}
	calls := r.Fake.CallsOf(domain.ModelNanoBanana)
	require.Len(t, calls, 3)
	for _, c := range calls {
		require.Len(t, c.Image.Inputs, 1)
		assert.Equal(t, "image/png", c.Image.Inputs[0].MimeType)
	}

	// What cannot be an input is refused before anything runs.
	for name, ref := range map[string]gentest.RefInput{
		"other project": {Source: gentest.FromProjectAsset, ProjectID: r.NewProject(), ID: assetID},
		"missing asset": {Source: gentest.FromProjectAsset, ProjectID: project, ID: "nope"},
		"missing item":  {Source: gentest.FromReferenceItem, ProjectID: project, ID: "nope"},
		"missing hist":  {Source: gentest.FromHistory, ID: "nope"},
		"missing stage": {Source: gentest.FromStaged, ID: "12345678.png"},
		"bad stage":     {Source: gentest.FromStaged, ID: "../etc/passwd"},
	} {
		req := imageReq("nano-banana")
		req.References = []gentest.RefInput{ref}
		var invalid *gterr.InvalidError
		_, err := r.Svc.Estimate(ctx, req)
		assert.True(t, errors.As(err, &invalid), name+": refused at the estimate")
		_, err = r.Svc.Run(ctx, req, gentest.Confirmation{EstimateID: "x", TotalUSD: 0})
		assert.Error(t, err, name+": and never run")
	}
	assert.Len(t, r.Fake.CallsOf(domain.ModelNanoBanana), 3)
}

func TestStageReferenceChecksTheImage(t *testing.T) {
	r := gentesttest.New(t)
	for name, data := range map[string][]byte{"empty": nil, "text": []byte("hello"), "huge": make([]byte, gentest.MaxImageBytes+1)} {
		_, _, err := r.Svc.StageReference(data)
		var invalid *gterr.InvalidError
		assert.ErrorAs(t, err, &invalid, name)
	}
	id, mime, err := r.Svc.StageReference(gentesttest.PNG)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	path, _, err := r.Svc.StagedFile(id)
	require.NoError(t, err)
	assert.FileExists(t, path)
	assert.Equal(t, filepath.Join(r.DataDir, "gentest", "_staging"), filepath.Dir(path))
}
