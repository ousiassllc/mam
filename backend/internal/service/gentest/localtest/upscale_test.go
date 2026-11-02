package localtest_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/localmodels"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
)

const (
	upscaleModel = "comfyui:box/up"
	twoPassModel = "comfyui:box/up2"
)

// upscaleBox is box with the model "gen" (text to image) and two upscale workflows: "up" (the upscale model alone) and "up2" (the two-pass
// finish).
func upscaleBox(t *testing.T, r *mmtest.Rig) *comfyuitest.Fake {
	t.Helper()
	f := comfyuitest.New(t)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	for name, tpl := range map[string]string{"gen": "txt2img-sdxl", "up": "upscale-nomos-uni-span", "up2": "upscale-2pass-sdxl-span"} {
		tm, ok := localcfg.TemplateByID(tpl)
		require.True(t, ok)
		require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: name, Model: tm.Model}))
	}
	return f
}

// generated makes an image result with the model "gen" and returns it once it succeeded.
func generated(t *testing.T, r *mmtest.Rig) record.ResultView {
	t.Helper()
	st := run(t, r, gentest.Request{
		Media: "image", Models: []string{"comfyui:box/gen"}, Prompts: []gentest.PromptInput{{Prompt: "a red fox", Negative: "blurry"}}, Seed: new(int64(1234)),
	})
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	return st.Results[0]
}

// enlarge starts an enlargement and waits for its batch to end.
func enlarge(t *testing.T, r *mmtest.Rig, req gentest.UpscaleRequest) gentest.BatchStatus {
	t.Helper()
	ref, err := r.Gentest.Upscale(ctx(), req)
	require.NoError(t, err)
	r.Start()
	var st gentest.BatchStatus
	require.Eventually(t, func() bool {
		st, err = r.Gentest.Batch(ctx(), ref.BatchID, true)
		return err == nil && st.Finished
	}, 30*time.Second, 20*time.Millisecond, "the enlargement did not end")
	require.Len(t, st.Results, 1)
	assert.Equal(t, ref.ResultID, st.Results[0].ID)
	return st
}

func scaleBy(t *testing.T, f *comfyuitest.Fake) float64 {
	t.Helper()
	for _, n := range f.Graph() {
		if n.ClassType == "ImageScaleBy" {
			var v float64
			require.NoError(t, json.Unmarshal(n.Inputs["scale_by"], &v))
			return v
		}
	}
	t.Fatal("the graph has no ImageScaleBy")
	return 0
}

func TestAResultIsEnlargedByOneActionIntoAFileOfItsOwnThatRemembersItsSource(t *testing.T) {
	r := mmtest.New(t)
	f := upscaleBox(t, r)
	src := generated(t, r)
	uploads := f.Count("POST /upload/image")

	st := enlarge(t, r, gentest.UpscaleRequest{ResultID: src.ID, Scale: 4, Model: upscaleModel})
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	got := st.Results[0]
	assert.NotEqual(t, src.BatchID, got.BatchID, "a batch of its own")
	assert.NotEqual(t, src.ID, got.ID)
	assert.Equal(t, upscaleModel, got.Model)
	assert.Equal(t, "free", got.CostBasis, "a local model has no bill")
	assert.Equal(t, 1, f.Count("POST /upload/image")-uploads, "the source picture went to the server")
	assert.Equal(t, 1.0, scaleBy(t, f), "a 4x model asked for 4x is left as it is")

	d, err := r.Gentest.Result(ctx(), got.ID)
	require.NoError(t, err)
	require.NotNil(t, d.Upscale)
	assert.Equal(t, src.ID, d.Upscale.SourceResultID)
	assert.Equal(t, 4, d.Upscale.Scale)
	assert.Equal(t, upscaleModel, d.Upscale.Model)
	assert.Equal(t, "model", d.Upscale.Mode)
	assert.Equal(t, 4, d.Upscale.Width, "the source is 1 x 1")
	assert.Equal(t, "image/png", d.MimeType)

	srcFile, err := r.Gentest.ResultFile(ctx(), src.ID)
	require.NoError(t, err)
	file, err := r.Gentest.ResultFile(ctx(), got.ID)
	require.NoError(t, err)
	assert.NotEqual(t, srcFile.Path, file.Path, "a file of its own")
	_, err = os.Stat(srcFile.Path)
	assert.NoError(t, err, "the source stays")
	again, err := r.Gentest.Result(ctx(), src.ID)
	require.NoError(t, err)
	assert.Nil(t, again.Upscale, "the source is not an enlargement")
}

func TestAScaleOfTwoShrinksTheModelsFourTimesToHalf(t *testing.T) {
	r := mmtest.New(t)
	f := upscaleBox(t, r)
	src := generated(t, r)
	st := enlarge(t, r, gentest.UpscaleRequest{ResultID: src.ID, Scale: 2, Model: upscaleModel})
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	assert.Equal(t, 0.5, scaleBy(t, f))
}

func TestWithoutAModelTheFirstOneThatNeedsNoPromptIsUsed(t *testing.T) {
	r := mmtest.New(t)
	upscaleBox(t, r)
	src := generated(t, r)
	st := enlarge(t, r, gentest.UpscaleRequest{ResultID: src.ID, Scale: 2})
	assert.Equal(t, upscaleModel, st.Results[0].Model, "the two-pass finish is chosen by name, not by default")
}

func TestTheTwoPassFinishTakesThePromptAndTheSeedOfItsSource(t *testing.T) {
	r := mmtest.New(t)
	f := upscaleBox(t, r)
	src := generated(t, r)
	st := enlarge(t, r, gentest.UpscaleRequest{ResultID: src.ID, Scale: 2, Model: twoPassModel})
	require.Equal(t, gentest.StateSucceeded, st.State, st.Results[0].Error)
	got := st.Results[0]
	assert.Equal(t, "a red fox", got.Prompt)
	assert.Equal(t, "blurry", got.NegativePrompt)
	require.NotNil(t, got.Seed)
	assert.EqualValues(t, 1234, *got.Seed, "the source's seed")
	g := f.Graph()
	var text, seed any
	for _, n := range g {
		switch n.ClassType {
		case "KSampler":
			require.NoError(t, json.Unmarshal(n.Inputs["seed"], &seed))
			var denoise float64
			require.NoError(t, json.Unmarshal(n.Inputs["denoise"], &denoise))
			assert.Less(t, denoise, 0.5, "a low denoise: the picture is finished, not drawn again")
		case "CLIPTextEncode":
			var s string
			if json.Unmarshal(n.Inputs["text"], &s) == nil && s == "a red fox" {
				text = s
			}
		case "VAEEncodeTiled", "VAEDecodeTiled":
			var tile int
			require.NoError(t, json.Unmarshal(n.Inputs["tile_size"], &tile))
			assert.LessOrEqual(t, tile, 512, "the VAE works in tiles that a 16 GB GPU holds")
		}
	}
	assert.EqualValues(t, 1234, seed)
	assert.Equal(t, "a red fox", text)
	assert.Equal(t, 0.5, scaleBy(t, f))
}

func TestAnEnlargementCanBeEnlargedAgain(t *testing.T) {
	r := mmtest.New(t)
	upscaleBox(t, r)
	src := generated(t, r)
	first := enlarge(t, r, gentest.UpscaleRequest{ResultID: src.ID, Scale: 2, Model: upscaleModel})
	second := enlarge(t, r, gentest.UpscaleRequest{ResultID: first.Results[0].ID, Scale: 2, Model: upscaleModel})
	d, err := r.Gentest.Result(ctx(), second.Results[0].ID)
	require.NoError(t, err)
	assert.Equal(t, first.Results[0].ID, d.Upscale.SourceResultID)
}

func TestAnEnlargementThatCannotRunIsRefusedBeforeAnythingIsQueued(t *testing.T) {
	r := mmtest.New(t)
	f := upscaleBox(t, r)
	src := generated(t, r)
	var invalid *gterr.InvalidError
	for name, req := range map[string]gentest.UpscaleRequest{
		"scale three":                     {ResultID: src.ID, Scale: 3, Model: upscaleModel},
		"no scale":                        {ResultID: src.ID, Model: upscaleModel},
		"a model that is not an upscaler": {ResultID: src.ID, Scale: 2, Model: "comfyui:box/gen"},
		"a model that is not there":       {ResultID: src.ID, Scale: 2, Model: "comfyui:box/missing"},
	} {
		_, err := r.Gentest.Upscale(ctx(), req)
		assert.ErrorAs(t, err, &invalid, name)
	}
	_, err := r.Gentest.Upscale(ctx(), gentest.UpscaleRequest{ResultID: "nope", Scale: 2})
	assert.ErrorIs(t, err, gterr.ErrNotFound)
	batches, err := r.Gentest.Batches(ctx(), 10)
	require.NoError(t, err)
	assert.Len(t, batches, 1, "only the batch that made the source")
	assert.Equal(t, 1, f.Count("POST /prompt"))
}

func TestWithNoUpscaleModelInstalledTheRequestSaysSo(t *testing.T) {
	r := mmtest.New(t)
	f := comfyuitest.New(t)
	require.NoError(t, r.Models.CreateEndpoint(ctx(), localmodels.EndpointInput{Name: "box", Kind: localcfg.KindComfyUI, BaseURL: f.URL()}))
	tm, _ := localcfg.TemplateByID("txt2img-sdxl")
	require.NoError(t, r.Models.CreateModel(ctx(), localmodels.ModelInput{Endpoint: "box", Name: "gen", Model: tm.Model}))
	src := generated(t, r)
	_, err := r.Gentest.Upscale(ctx(), gentest.UpscaleRequest{ResultID: src.ID, Scale: 2})
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "モデルマネージャー")
	assert.Empty(t, r.Gentest.Upscalers(ctx()))
}

func TestAnUpscaleWorkflowIsNoModelToGiveAPromptTo(t *testing.T) {
	r := mmtest.New(t)
	upscaleBox(t, r)
	var ids []string
	for _, m := range r.Gentest.Models(ctx(), "image") {
		ids = append(ids, m.ID)
	}
	assert.Contains(t, ids, "comfyui:box/gen")
	assert.NotContains(t, ids, upscaleModel)
	assert.NotContains(t, ids, twoPassModel)
	_, err := r.Gentest.Model(ctx(), "image", upscaleModel)
	assert.ErrorIs(t, err, gterr.ErrNotFound)

	var up []string
	for _, u := range r.Gentest.Upscalers(ctx()) {
		up = append(up, u.ID)
		assert.True(t, u.Available, u.ID)
	}
	assert.Equal(t, []string{upscaleModel, twoPassModel}, up)
}

func TestAnOutputOverTheLimitOfThePixelsIsRefused(t *testing.T) {
	r := mmtest.New(t)
	f := upscaleBox(t, r)
	var big bytes.Buffer
	require.NoError(t, png.Encode(&big, image.NewGray(image.Rect(0, 0, 2100, 2100))))
	f.OutputBody = big.Bytes()
	src := generated(t, r)

	_, err := r.Gentest.Upscale(ctx(), gentest.UpscaleRequest{ResultID: src.ID, Scale: 4, Model: upscaleModel})
	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid, "2100 x 4 squared is over 8192 x 8192")
	assert.Contains(t, invalid.Message, "上限")

	_, err = r.Gentest.Upscale(ctx(), gentest.UpscaleRequest{ResultID: src.ID, Scale: 2, Model: twoPassModel})
	require.ErrorAs(t, err, &invalid, "the two-pass finish samples the whole picture: its limit is lower (4096 x 4096)")

	_, err = r.Gentest.Upscale(ctx(), gentest.UpscaleRequest{ResultID: src.ID, Scale: 2, Model: upscaleModel})
	assert.NoError(t, err, "2x of the same picture is within the limit of the upscale model alone")
}
