package upscale_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg/upscale"
)

func template(t *testing.T, id string) localcfg.Model {
	t.Helper()
	tpl, ok := localcfg.TemplateByID(id)
	require.True(t, ok, id)
	return tpl.Model
}

// withSpec is the model with a changed copy of its workflow's marker.
func withSpec(m localcfg.Model, f func(*upscale.Spec)) localcfg.Model {
	w := *m.Workflow
	s := *w.Upscale
	f(&s)
	w.Upscale = &s
	m.Workflow = &w
	return m
}

func TestTheUpscaleTemplatesAreValidWorkflowsThatEnlargeWithTheModel(t *testing.T) {
	for id, mode := range map[string]upscale.Mode{"upscale-nomos-uni-span": upscale.Model, "upscale-nomos-webphoto-esrgan": upscale.Model, "upscale-2pass-sdxl-span": upscale.TwoPass} {
		m := template(t, id)
		assert.True(t, m.IsUpscale(), id)
		assert.Equal(t, mode, m.Workflow.Upscale.Mode, id)
		assert.Equal(t, 4, m.Workflow.Upscale.Native, id)
		assert.Equal(t, localcfg.MediaImage, m.Media, "an upscale workflow is an image workflow on the endpoint: %s", id)
		assert.Empty(t, localcfg.ValidateSaved(m), id)
		assert.Equal(t, 1, m.Workflow.Count(localcfg.ParamReferenceImage), id)
		assert.Equal(t, 1, m.Workflow.Count(localcfg.ParamUpscaleRatio), id)
		assert.Equal(t, mode == upscale.TwoPass, m.Workflow.Has(localcfg.ParamPrompt), "only the finish takes a prompt: %s", id)
		assert.True(t, m.ImageInputs(localcfg.KindComfyUI), "the picture is given as a reference image: %s", id)
	}
}

// A 16 GB card must not fall over: ImageUpscaleWithModel tiles the picture itself, and the finish encodes and decodes in tiles of 512
// (the VAE of an enlarged picture does not fit as a whole) with a low denoise, so that the picture stays the picture.
func TestTheTwoPassFinishWorksInTilesWithALowDenoise(t *testing.T) {
	g, err := localcfg.ParseGraph(template(t, "upscale-2pass-sdxl-span").Workflow.Graph)
	require.NoError(t, err)
	classes := map[string]int{}
	for _, n := range g {
		classes[n.ClassType]++
		assert.NotContains(t, []string{"VAEEncode", "VAEDecode"}, n.ClassType, "the whole-picture VAE is the one that runs out of memory")
	}
	assert.Equal(t, map[string]int{"VAEEncodeTiled": 1, "VAEDecodeTiled": 1, "ImageUpscaleWithModel": 1}, map[string]int{
		"VAEEncodeTiled": classes["VAEEncodeTiled"], "VAEDecodeTiled": classes["VAEDecodeTiled"], "ImageUpscaleWithModel": classes["ImageUpscaleWithModel"],
	})
}

func TestTheRatioFitsTheRequestedScaleToTheModelsFactor(t *testing.T) {
	assert.Equal(t, 1.0, upscale.Spec{Native: 4}.Ratio(4))
	assert.Equal(t, 0.5, upscale.Spec{Native: 4}.Ratio(2))
	assert.Equal(t, 1.0, upscale.Spec{Native: 4}.Ratio(0), "no scale: the model's own")
	assert.Equal(t, 2.0, upscale.Spec{Native: 2}.Ratio(4))
}

func TestAnUpscaleMarkerThatDoesNotFitItsWorkflowIsRefused(t *testing.T) {
	base := template(t, "upscale-nomos-uni-span")
	for name, bad := range map[string]localcfg.Model{
		"no factor":                 withSpec(base, func(s *upscale.Spec) { s.Native = 0 }),
		"unknown mode":              withSpec(base, func(s *upscale.Spec) { s.Mode = "magic" }),
		"no label":                  withSpec(base, func(s *upscale.Spec) { s.Label = "" }),
		"negative size":             withSpec(base, func(s *upscale.Spec) { s.MaxOutputPixels = -1 }),
		"two-pass without a prompt": withSpec(base, func(s *upscale.Spec) { s.Mode = upscale.TwoPass }),
	} {
		assert.NotEmpty(t, localcfg.ValidateSaved(bad), name)
	}

	video := base
	video.Media = localcfg.MediaVideo
	assert.NotEmpty(t, localcfg.ValidateSaved(video), "only an image workflow enlarges")

	plain := template(t, "txt2img-sdxl")
	w := *plain.Workflow
	w.Assignments = append(append([]localcfg.Assignment{}, w.Assignments...), localcfg.Assignment{Param: localcfg.ParamUpscaleRatio, Node: "5", Input: "batch_size"})
	plain.Workflow = &w
	errs := localcfg.ValidateSaved(plain)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "upscale_ratio")
}

func TestAnUpscaleWorkflowIsNotAModelOfThePrompts(t *testing.T) {
	endpoints := map[string]localcfg.Endpoint{"box": {Kind: localcfg.KindComfyUI, BaseURL: "http://127.0.0.1:8188"}}
	models := map[string]localcfg.Model{"box/up": template(t, "upscale-nomos-uni-span"), "box/gen": template(t, "txt2img-sdxl")}
	assert.Equal(t, []string{"comfyui:box/gen"}, localcfg.IDs(endpoints, models, localcfg.MediaImage))
}
