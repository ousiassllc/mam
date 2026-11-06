package recipetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/sampling"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/recipe"
)

// full is a request that names every setting a recipe keeps, with a prompt and an input image that it must not.
func full() gentest.Request {
	r := request(exposedModel)
	r.Prompts = []gentest.PromptInput{{Prompt: "secret prompt", Negative: "blurry"}}
	r.Sampling = sampling.Settings{Steps: ptr(31), CFG: ptr(5.5), Sampler: "euler", Scheduler: "normal"}
	r.Seed = ptr(int64(4242))
	r.AspectRatio = "16:9"
	r.LoRA = &gentest.LoRAInput{Name: "detail.safetensors", Strength: ptr(0.5)}
	r.References = []gentest.RefInput{{Source: gentest.FromHistory, ID: "res-1", Strength: ptr(0.35)}}
	r.Exposed = map[string]any{gentesttest.ExposedDenoise: 0.7, gentesttest.ExposedPrefix: "fox"}
	return r
}

func TestARecipeKeepsTheSettingsAndNotThePromptOrTheImages(t *testing.T) {
	r := exposedRig(t)
	svc := recipe.New(r.Svc)
	v, err := svc.Save(ctx(), "  Fox look ", full(), true)
	require.NoError(t, err)
	assert.Equal(t, "Fox look", v.Name)
	assert.True(t, v.Easy)
	assert.True(t, v.Usable, v.Reason)
	p := v.Params
	assert.Equal(t, exposedModel, p.Model)
	assert.Equal(t, sampling.Settings{Steps: ptr(31), CFG: ptr(5.5), Sampler: "euler", Scheduler: "normal"}, p.Sampling)
	assert.Equal(t, ptr(int64(4242)), p.Seed)
	assert.Equal(t, "blurry", p.Negative)
	assert.Equal(t, "16:9", p.AspectRatio)
	assert.Equal(t, "detail.safetensors", p.LoRA.Name)
	assert.InDelta(t, 0.5, *p.LoRA.Strength, 0)
	assert.InDelta(t, 0.35, *p.ReferenceStrength, 0)
	assert.Equal(t, map[string]any{gentesttest.ExposedDenoise: 0.7, gentesttest.ExposedPrefix: "fox"}, p.Exposed)

	// what is kept in the table: no prompt text, no image
	var raw string
	require.NoError(t, r.DB.Reader().QueryRowContext(ctx(), `SELECT params FROM gentest_recipe`).Scan(&raw))
	assert.NotContains(t, raw, "secret prompt")
	assert.NotContains(t, raw, "res-1")

	// it comes back the same after a list (a restart reads the table)
	list, err := svc.List(ctx())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, v.Params.Sampling, list[0].Params.Sampling)
	assert.Equal(t, v.Params.Exposed, list[0].Params.Exposed)
}

// "選んで同じ設定になる": a recipe applied to a request that names only the prompt gives the model and every setting; what the request names wins.
func TestApplyingARecipeGivesTheSameSettingsAndWhatTheRequestNamesWins(t *testing.T) {
	r := exposedRig(t)
	svc := recipe.New(r.Svc)
	v, err := svc.Save(ctx(), "Fox", full(), false)
	require.NoError(t, err)

	bare := gentest.Request{Media: "image", Prompts: []gentest.PromptInput{{Prompt: "a cat"}, {Prompt: "a dog", Negative: "mine"}}, References: []gentest.RefInput{{Source: gentest.FromHistory, ID: "x"}}}
	got, applied, err := svc.Apply(ctx(), bare, v.ID)
	require.NoError(t, err)
	assert.Equal(t, v.ID, applied.ID)
	assert.Equal(t, []string{exposedModel}, got.Models)
	assert.Equal(t, *full().Sampling.Steps, *got.Sampling.Steps)
	assert.Equal(t, int64(4242), *got.Seed)
	assert.Equal(t, "16:9", got.AspectRatio)
	assert.Equal(t, "blurry", got.Prompts[0].Negative)
	assert.Equal(t, "mine", got.Prompts[1].Negative, "a negative prompt that is typed is kept")
	assert.InDelta(t, 0.35, *got.References[0].Strength, 0)
	assert.Equal(t, "a cat", got.Prompts[0].Prompt, "the prompt is the request's own")
	assert.Nil(t, bare.Models, "the request is not modified")

	// and it runs: the call has the values of the recipe
	ref, err := r.Confirmed(withoutRefs(got))
	require.NoError(t, err)
	require.Equal(t, gentest.StateSucceeded, r.Wait(ref.BatchID).State)
	call := r.Fake.CallsOf(gentesttest.ExposedModel)[0].Image
	assert.Equal(t, 31, *call.Options.Sampling.Steps)
	assert.InDelta(t, 0.7, call.Options.Exposed[gentesttest.ExposedDenoise], 0)
	assert.Equal(t, int64(4242), *call.Seed)

	own := gentest.Request{Media: "image", Prompts: bare.Prompts, Sampling: sampling.Settings{Steps: ptr(10)}, AspectRatio: "1:1", Exposed: map[string]any{gentesttest.ExposedDenoise: 0.2}}
	got, _, err = svc.Apply(ctx(), own, v.ID)
	require.NoError(t, err)
	assert.Equal(t, 10, *got.Sampling.Steps)
	assert.InDelta(t, 5.5, *got.Sampling.CFG, 0, "a value the request leaves out comes from the recipe")
	assert.Equal(t, "1:1", got.AspectRatio)
	assert.InDelta(t, 0.2, got.Exposed[gentesttest.ExposedDenoise], 0)
	assert.Equal(t, "fox", got.Exposed[gentesttest.ExposedPrefix])

	_, _, err = svc.Apply(ctx(), request(exposedModel), v.ID)
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid, "models and a recipe together")
}

func withoutRefs(r gentest.Request) gentest.Request {
	r.References = nil
	return r
}

func TestRenameEasyAndDelete(t *testing.T) {
	r := exposedRig(t)
	svc := recipe.New(r.Svc)
	a, err := svc.Save(ctx(), "A", full(), false)
	require.NoError(t, err)
	b, err := svc.Save(ctx(), "B", full(), false)
	require.NoError(t, err)

	_, err = svc.Save(ctx(), "a", full(), false)
	var conflict *gterr.ConflictError
	assert.ErrorAs(t, err, &conflict, "a name is taken ignoring case")
	_, err = svc.Rename(ctx(), b.ID, "A")
	assert.ErrorAs(t, err, &conflict)
	renamed, err := svc.Rename(ctx(), b.ID, "Beta")
	require.NoError(t, err)
	assert.Equal(t, "Beta", renamed.Name)
	_, err = svc.Rename(ctx(), b.ID, "  ")
	var invalid *gterr.InvalidError
	assert.ErrorAs(t, err, &invalid)

	easy, err := svc.EasyChoices(ctx())
	require.NoError(t, err)
	assert.Empty(t, easy)
	_, err = svc.SetEasy(ctx(), a.ID, true)
	require.NoError(t, err)
	easy, err = svc.EasyChoices(ctx())
	require.NoError(t, err)
	require.Len(t, easy, 1)
	assert.Equal(t, a.ID, easy[0].ID)
	_, err = svc.SetEasy(ctx(), a.ID, false)
	require.NoError(t, err)
	easy, _ = svc.EasyChoices(ctx())
	assert.Empty(t, easy, "taken out of the easy tier")

	require.NoError(t, svc.Delete(ctx(), a.ID))
	assert.ErrorIs(t, svc.Delete(ctx(), a.ID), gterr.ErrNotFound)
	_, err = svc.Get(ctx(), a.ID)
	assert.ErrorIs(t, err, gterr.ErrNotFound)
	list, _ := svc.List(ctx())
	assert.Len(t, list, 1)
}

func TestARecipeThatCannotBeSavedIsRefused(t *testing.T) {
	r := exposedRig(t)
	svc := recipe.New(r.Svc)
	two := full()
	two.Models = []string{exposedModel, string(gentesttest.SamplingModel)}
	video := full()
	video.Media = gentest.MediaVideo
	bad := full()
	bad.Sampling.Steps = ptr(9999)
	badAspect := full()
	badAspect.AspectRatio = "wide"
	unknownKey := full()
	unknownKey.Exposed = map[string]any{"3.steps": 3}
	noModel := full()
	noModel.Models = []string{"nope"}
	for name, req := range map[string]gentest.Request{"two models": two, "a video": video, "steps out of range": bad, "an aspect": badAspect, "an exposed key the model lacks": unknownKey, "a model that is not there": noModel} {
		_, err := svc.Save(ctx(), "x", req, false)
		var invalid *gterr.InvalidError
		assert.ErrorAs(t, err, &invalid, name)
	}
	_, err := svc.Save(ctx(), "", full(), false)
	assert.Error(t, err, "a name is required")
	list, _ := svc.List(ctx())
	assert.Empty(t, list)
}

// A recipe whose model is gone is kept and says why; the age gate and the commercial filter follow the model.
func TestARecipeWhoseModelCannotBeUsedIsKeptWithTheReason(t *testing.T) {
	hidden := false
	r := gentesttest.New(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel(),
		gentesttest.WithHidden(func(id string) bool { return hidden && id == exposedModel }))
	svc := recipe.New(r.Svc)
	v, err := svc.Save(ctx(), "Fox", full(), true)
	require.NoError(t, err)
	require.True(t, v.Usable)

	hidden = true // the age gate closed
	got, err := svc.Get(ctx(), v.ID)
	require.NoError(t, err)
	assert.False(t, got.Usable)
	assert.Contains(t, got.Reason, "成人向け")
	_, _, err = svc.Apply(ctx(), gentest.Request{Media: "image", Prompts: full().Prompts}, v.ID)
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, got.Reason)

	hidden = false
	got, _ = svc.Get(ctx(), v.ID)
	assert.True(t, got.Usable)

	// the model is deleted: the recipe stays
	gone := gentesttest.New(t, gentesttest.WithTemplateModels())
	gsvc := recipe.New(gone.Svc)
	row := v.Recipe
	row.Params.Model = "comfyui:bundled-comfyui/not-installed"
	require.NoError(t, saveRaw(t, gone, row))
	list, err := gsvc.List(ctx())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.False(t, list[0].Usable)
	assert.Contains(t, list[0].Reason, "導入されていない")
	assert.Equal(t, "Fox", list[0].Name)
}

// The age gate closed (Issue #992): the list, the easy tier's choices and a single read name neither the model nor a setting of a recipe of a hidden model;
// the recipe is kept as it was (a rename or a change of the easy choice while the gate is closed does not blank it) and comes back when the gate opens.
func TestARecipeOfAHiddenModelShowsNeitherTheModelNorItsSettings(t *testing.T) {
	hidden := false
	r := gentesttest.New(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel(),
		gentesttest.WithHidden(func(id string) bool { return hidden && id == exposedModel }))
	svc := recipe.New(r.Svc)
	saved, err := svc.Save(ctx(), "Fox", full(), true)
	require.NoError(t, err)
	require.NotEmpty(t, saved.ModelLabel)

	hidden = true
	list, err := svc.List(ctx())
	require.NoError(t, err)
	choices, err := svc.EasyChoices(ctx())
	require.NoError(t, err)
	one, err := svc.Get(ctx(), saved.ID)
	require.NoError(t, err)
	renamed, err := svc.Rename(ctx(), saved.ID, "Fox 2")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Len(t, choices, 1, "the recipe stays among the easy choices, with the reason it cannot be used")
	for name, v := range map[string]recipe.View{"List": list[0], "EasyChoices": choices[0], "Get": one, "Rename": renamed} {
		assert.Equal(t, recipe.Params{}, v.Params, name)
		assert.Equal(t, "成人向けのモデル", v.ModelLabel, name)
		assert.NotContains(t, v.ModelLabel, saved.ModelLabel, name)
		assert.False(t, v.Usable, name)
		assert.Contains(t, v.Reason, "成人向け", name)
		assert.NotContains(t, v.Reason, saved.ModelLabel, name)
	}
	assert.Equal(t, "Fox 2", renamed.Name)
	_, err = svc.SetEasy(ctx(), saved.ID, false)
	require.NoError(t, err)

	hidden = false
	back, err := svc.Get(ctx(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, saved.Params, back.Params, "the settings were kept while the gate was closed")
	assert.Equal(t, "Fox 2", back.Name)
	assert.False(t, back.Easy)
	assert.True(t, back.Usable, back.Reason)
}

// The settings that keep a model out of the easy tier's choices keep it out of a recipe as well (the same check).
func TestARecipeFollowsThePlatformCheckOfTheEasyTier(t *testing.T) {
	warn := ""
	r := gentesttest.New(t, gentesttest.WithTemplateModels(), gentesttest.WithExposedModel(),
		gentesttest.WithPlatformWarning(func(context.Context, string) string { return warn }))
	svc := recipe.New(r.Svc)
	v, err := svc.Save(ctx(), "Fox", full(), true)
	require.NoError(t, err)
	assert.True(t, v.Usable)
	warn = "no GPU"
	got, _ := svc.Get(ctx(), v.ID)
	assert.False(t, got.Usable)
	assert.Contains(t, got.Reason, "OS・GPU")
}

// #1096: the complexity and the GIF settings of GPT Image 2.6 are part of a recipe, so that using it again does not silently drop them; a request's own wins.
func TestARecipeKeepsTheGPTImageSettingsAndApplyingItGivesThemBack(t *testing.T) {
	r := gentesttest.New(t)
	svc := recipe.New(r.Svc)
	flare := string(domain.ModelGPTImage26Flare)
	o := gptimage.Options{Complexity: gptimage.ComplexityLow, Format: gptimage.FormatGIF, FrameCount: 6, FrameDurationMS: 120}
	save := request(flare)
	save.GPTImage = o
	v, err := svc.Save(ctx(), "Loop", save, false)
	require.NoError(t, err)
	require.NotNil(t, v.Params.GPT)
	assert.Equal(t, o, *v.Params.GPT)

	list, err := svc.List(ctx())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotNil(t, list[0].Params.GPT, "it comes back after a restart")
	assert.Equal(t, o, *list[0].Params.GPT)

	got, _, err := svc.Apply(ctx(), gentest.Request{Media: "image", Prompts: []gentest.PromptInput{{Prompt: "a cat"}}}, v.ID)
	require.NoError(t, err)
	assert.Equal(t, o, got.GPTImage)

	own := gptimage.Options{Complexity: gptimage.ComplexityHigh}
	got, _, err = svc.Apply(ctx(), gentest.Request{Media: "image", Prompts: []gentest.PromptInput{{Prompt: "a cat"}}, GPTImage: own}, v.ID)
	require.NoError(t, err)
	assert.Equal(t, own, got.GPTImage, "what the request names wins")

	// a recipe of a model that does not take them is not saved without them
	bad := request(string(domain.ModelGPTImage2))
	bad.GPTImage = o
	_, err = svc.Save(ctx(), "Nope", bad, false)
	var inv *gterr.InvalidError
	require.ErrorAs(t, err, &inv)
	assert.Contains(t, inv.Message, "対応していません")
}
